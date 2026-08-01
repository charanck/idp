"""
Integration tests for authentication/oauth_service.py.

External HTTP calls (authlib's OAuth2Session and requests) are mocked - these
tests exercise our own business logic (error translation, user
matching/creation, token bookkeeping), not third-party libraries or the
network.
"""
from datetime import datetime, timedelta
from unittest.mock import MagicMock, patch

import pytest
import requests
from authlib.integrations.base_client.errors import OAuthError
from django.contrib.auth import get_user_model

from authentication.oauth_models import OAuthProvider, OAuthUserToken
from authentication.oauth_service import OAuthService

pytestmark = pytest.mark.django_db

User = get_user_model()
service = OAuthService()


@pytest.fixture
def provider():
    return OAuthProvider.objects.create(
        name="Google",
        client_id="client-id",
        client_secret="client-secret",
        authorization_url="https://provider.example/authorize",
        token_url="https://provider.example/token",
        userinfo_url="https://provider.example/userinfo",
    )


class TestExchangeCodeForToken:
    def test_returns_token_data_on_success(self, provider):
        with patch("authentication.oauth_service.OAuth2Session") as mock_session_cls:
            mock_session_cls.return_value.fetch_token.return_value = {"access_token": "tok"}

            result = service.exchange_code_for_token(provider, "auth-code", "https://app/callback")

        assert result == {"access_token": "tok"}

    def test_translates_oauth_error_from_provider(self, provider):
        with patch("authentication.oauth_service.OAuth2Session") as mock_session_cls:
            mock_session_cls.return_value.fetch_token.side_effect = OAuthError(description="bad code")

            with pytest.raises(ValueError, match="rejected the authorization code"):
                service.exchange_code_for_token(provider, "bad-code", "https://app/callback")

    def test_translates_network_error(self, provider):
        with patch("authentication.oauth_service.OAuth2Session") as mock_session_cls:
            mock_session_cls.return_value.fetch_token.side_effect = requests.ConnectionError("down")

            with pytest.raises(ValueError, match="Could not reach"):
                service.exchange_code_for_token(provider, "code", "https://app/callback")


class TestGetUserInfo:
    def test_raises_when_provider_has_no_userinfo_url(self, provider):
        provider.userinfo_url = ""
        provider.save()

        with pytest.raises(ValueError, match="userinfo_url"):
            service.get_user_info(provider, "access-token")

    def test_returns_parsed_json_on_success(self, provider):
        mock_response = MagicMock()
        mock_response.json.return_value = {"sub": "123", "email": "a@example.com"}
        mock_response.raise_for_status.return_value = None

        with patch("authentication.oauth_service.requests.get", return_value=mock_response):
            result = service.get_user_info(provider, "access-token")

        assert result == {"sub": "123", "email": "a@example.com"}

    def test_raises_on_http_error(self, provider):
        with patch("authentication.oauth_service.requests.get", side_effect=requests.HTTPError("500")):
            with pytest.raises(ValueError, match="Could not fetch user info"):
                service.get_user_info(provider, "access-token")

    def test_raises_on_unparseable_response(self, provider):
        mock_response = MagicMock()
        mock_response.json.side_effect = ValueError("not json")
        mock_response.raise_for_status.return_value = None

        with patch("authentication.oauth_service.requests.get", return_value=mock_response):
            with pytest.raises(ValueError, match="invalid user info response"):
                service.get_user_info(provider, "access-token")


class TestAuthenticateOrCreateUser:
    def test_raises_when_provider_user_id_missing(self, provider):
        with pytest.raises(ValueError, match="user ID"):
            service.authenticate_or_create_user(provider, {}, {"email": "a@example.com"})

    def test_raises_when_email_missing(self, provider):
        with pytest.raises(ValueError, match="email"):
            service.authenticate_or_create_user(provider, {}, {"sub": "123"})

    def test_creates_new_user_when_none_exists(self, provider):
        token_data = {"access_token": "tok", "refresh_token": "rtok", "expires_in": 3600}
        user_info = {"sub": "provider-user-1", "email": "newperson@example.com"}

        user, oauth_token = service.authenticate_or_create_user(provider, token_data, user_info)

        assert user.email == "newperson@example.com"
        assert user.has_usable_password() is False
        assert user.is_active is False
        assert oauth_token.provider_user_id == "provider-user-1"
        assert oauth_token.access_token == "tok"

    def test_raises_when_auto_create_disabled_and_user_unknown(self, provider):
        provider.auto_create_users = False
        provider.save()
        token_data = {"access_token": "tok"}
        user_info = {"sub": "provider-user-1", "email": "newperson@example.com"}

        with pytest.raises(ValueError, match="auto-creation is disabled"):
            service.authenticate_or_create_user(provider, token_data, user_info)

    def test_links_existing_user_by_email_when_no_oauth_token_yet(self, provider):
        existing_user = User.objects.create(email="existing@example.com", username="existing", is_active=True)
        token_data = {"access_token": "tok"}
        user_info = {"sub": "provider-user-1", "email": "existing@example.com"}

        user, oauth_token = service.authenticate_or_create_user(provider, token_data, user_info)

        assert user == existing_user
        assert oauth_token.user == existing_user

    def test_resolves_username_collision_when_creating_user(self, provider):
        User.objects.create(email="other@example.com", username="newperson")
        token_data = {"access_token": "tok"}
        user_info = {"sub": "provider-user-1", "email": "newperson@example.com"}

        user, _ = service.authenticate_or_create_user(provider, token_data, user_info)

        assert user.username == "newperson1"

    def test_reuses_existing_oauth_token_and_updates_it(self, provider):
        user = User.objects.create(email="existing@example.com", username="existing", is_active=True)
        OAuthUserToken.objects.create(
            user=user,
            provider=provider,
            access_token="old-token",
            provider_user_id="provider-user-1",
            provider_user_email="existing@example.com",
            expires_at=datetime.utcnow() + timedelta(hours=1),
        )
        token_data = {"access_token": "new-token", "refresh_token": "new-refresh", "expires_in": 7200}
        user_info = {"sub": "provider-user-1", "email": "existing@example.com"}

        returned_user, oauth_token = service.authenticate_or_create_user(provider, token_data, user_info)

        assert returned_user == user
        assert oauth_token.access_token == "new-token"
        assert oauth_token.refresh_token == "new-refresh"
        assert OAuthUserToken.objects.count() == 1
