"""
Integration tests for the OAuth login callback in web_ui/views.py - specifically
that django.contrib.auth.login() is never called for a not-yet-activated user,
since login() itself performs no is_active check.
"""
from unittest.mock import patch

import pytest
from django.test import Client

from authentication.models import User
from authentication.oauth_models import OAuthProvider

pytestmark = pytest.mark.django_db


@pytest.fixture
def provider():
    return OAuthProvider.objects.create(
        name="Google",
        client_id="client-id",
        client_secret="client-secret",
        authorization_url="https://provider.example/authorize",
        token_url="https://provider.example/token",
        userinfo_url="https://provider.example/userinfo",
        is_active=True,
        auto_create_users=True,
    )


def _run_callback(client, provider):
    session = client.session
    session[f"oauth_state_{provider.id}"] = "expected-state"
    session.save()

    with patch("web_ui.views.oauth_service.exchange_code_for_token", return_value={"access_token": "tok"}), \
         patch("web_ui.views.oauth_service.get_user_info", return_value={"sub": "1", "email": "newperson@example.com"}):
        return client.get(f"/oauth/callback/{provider.id}/?code=abc&state=expected-state")


class TestOAuthCallbackActivation:
    def test_rejects_login_for_newly_auto_created_user(self, provider):
        client = Client()

        response = _run_callback(client, provider)

        assert response.status_code == 302
        assert response.url == "/login/"
        assert "_auth_user_id" not in client.session
        user = User.objects.get(email="newperson@example.com")
        assert user.is_active is False

    def test_rejects_login_for_existing_but_inactive_user(self, provider):
        User.objects.create(email="newperson@example.com", username="newperson", is_active=False)
        client = Client()

        response = _run_callback(client, provider)

        assert response.status_code == 302
        assert "_auth_user_id" not in client.session

    def test_allows_login_for_activated_user(self, provider):
        User.objects.create(email="newperson@example.com", username="newperson", is_active=True)
        client = Client()

        response = _run_callback(client, provider)

        assert response.status_code == 302
        assert response.url == "/dashboard/"
        assert "_auth_user_id" in client.session
