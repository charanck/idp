"""
Integration tests for authentication/services.py (AuthService) - uses the
sqlite test database (see control_plane_project/test_settings.py).
"""
import uuid

import pytest

from authentication.models import ServiceClient, User
from authentication.services import AuthService

pytestmark = pytest.mark.django_db

service = AuthService()


class TestRegisterUser:
    def test_creates_inactive_user_with_hashed_password(self):
        user = service.register_user("new@example.com", "password123")

        assert user.email == "new@example.com"
        assert user.is_active is False
        assert user.password != "password123"
        assert user.check_password("password123")

    def test_generates_username_when_not_provided(self):
        user = service.register_user("new@example.com", "password123")
        assert user.username.startswith("new")

    def test_uses_provided_username(self):
        user = service.register_user("new@example.com", "password123", username="customname")
        assert user.username == "customname"

    def test_raises_when_email_already_registered(self):
        service.register_user("dup@example.com", "password123")

        with pytest.raises(ValueError, match="already exists"):
            service.register_user("dup@example.com", "password123")


class TestAuthenticateUser:
    def test_returns_user_for_correct_credentials(self):
        user = service.register_user("login@example.com", "password123")
        user.is_active = True
        user.save()

        user = service.authenticate_user("login@example.com", "password123")

        assert user is not None
        assert user.email == "login@example.com"

    def test_returns_none_for_wrong_password(self):
        user = service.register_user("login@example.com", "password123")
        user.is_active = True
        user.save()

        assert service.authenticate_user("login@example.com", "wrong-password") is None

    def test_returns_none_for_unknown_email(self):
        assert service.authenticate_user("nobody@example.com", "password123") is None

    def test_returns_none_for_newly_registered_user_pending_activation(self):
        service.register_user("login@example.com", "password123")

        assert service.authenticate_user("login@example.com", "password123") is None

    def test_returns_none_for_inactive_user(self):
        user = service.register_user("login@example.com", "password123")
        user.is_active = False
        user.save()

        assert service.authenticate_user("login@example.com", "password123") is None


class TestGetUserById:
    def test_returns_active_user(self):
        user = service.register_user("lookup@example.com", "password123")
        user.is_active = True
        user.save()

        found = service.get_user_by_id(str(user.id))

        assert found == user

    def test_returns_none_for_inactive_user(self):
        user = service.register_user("lookup@example.com", "password123")
        user.is_active = False
        user.save()

        assert service.get_user_by_id(str(user.id)) is None

    def test_returns_none_for_unknown_id(self):
        assert service.get_user_by_id(str(uuid.uuid4())) is None

    def test_returns_none_for_malformed_id(self):
        assert service.get_user_by_id("not-a-uuid") is None


class TestCreateServiceClient:
    def test_creates_client_with_working_api_key(self):
        credentials = service.create_service_client("payments-service")

        assert credentials.client.name == "payments-service"
        assert credentials.client.is_active is True
        assert credentials.client.api_key_id in credentials.api_key
        assert "." in credentials.api_key

    def test_generates_distinct_encryption_key_per_client(self):
        creds_a = service.create_service_client("service-a")
        creds_b = service.create_service_client("service-b")

        assert creds_a.client.encryption_key != creds_b.client.encryption_key

    def test_raises_when_name_already_taken(self):
        service.create_service_client("payments-service")

        with pytest.raises(ValueError, match="already exists"):
            service.create_service_client("payments-service")

    def test_does_not_store_plaintext_secret(self):
        credentials = service.create_service_client("payments-service")
        secret = credentials.api_key.split(".", 1)[1]

        assert secret not in credentials.client.api_key_hash


class TestAuthenticateServiceApiKey:
    def test_authenticates_valid_key(self):
        credentials = service.create_service_client("payments-service")

        client = service.authenticate_service_api_key(credentials.api_key)

        assert client == credentials.client

    def test_rejects_key_without_separator(self):
        assert service.authenticate_service_api_key("no-dot-here") is None

    def test_rejects_empty_key(self):
        assert service.authenticate_service_api_key("") is None

    def test_rejects_key_with_empty_secret(self):
        credentials = service.create_service_client("payments-service")
        key_id = credentials.api_key.split(".", 1)[0]

        assert service.authenticate_service_api_key(f"{key_id}.") is None

    def test_rejects_unknown_key_id(self):
        assert service.authenticate_service_api_key("sk_live_deadbeef.some-secret") is None

    def test_rejects_wrong_secret_for_known_key_id(self):
        credentials = service.create_service_client("payments-service")
        key_id = credentials.api_key.split(".", 1)[0]

        assert service.authenticate_service_api_key(f"{key_id}.wrong-secret") is None

    def test_rejects_inactive_client(self):
        credentials = service.create_service_client("payments-service")
        credentials.client.is_active = False
        credentials.client.save()

        assert service.authenticate_service_api_key(credentials.api_key) is None


class TestGetServiceClientById:
    def test_returns_active_client(self):
        credentials = service.create_service_client("payments-service")

        assert service.get_service_client_by_id(str(credentials.client.id)) == credentials.client

    def test_returns_none_for_inactive_client(self):
        credentials = service.create_service_client("payments-service")
        credentials.client.is_active = False
        credentials.client.save()

        assert service.get_service_client_by_id(str(credentials.client.id)) is None

    def test_returns_none_for_malformed_id(self):
        assert service.get_service_client_by_id("not-a-uuid") is None
