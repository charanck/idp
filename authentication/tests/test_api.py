"""
Integration tests for the Ninja endpoints in authentication/api.py, exercised
through the real HTTP router (client.post/get) against the sqlite test DB.
"""
import pytest

from authentication.models import User
from authentication.services import AuthService
from common.jwt_utils import create_access_token

pytestmark = pytest.mark.django_db

auth_service = AuthService()


@pytest.fixture
def admin_user():
    user = User.objects.create(email="admin@example.com", username="admin", is_staff=True, is_active=True)
    user.set_password("password123")
    user.save()
    return user


@pytest.fixture
def admin_headers(admin_user):
    token = create_access_token(subject=str(admin_user.id), token_type="user")
    return {"HTTP_AUTHORIZATION": f"Bearer {token}"}


class TestRegisterEndpoint:
    def test_requires_admin_auth(self, client):
        response = client.post(
            "/api/v1/auth/register",
            data={"email": "new@example.com", "password": "password123"},
            content_type="application/json",
        )
        assert response.status_code == 401

    def test_creates_user_as_admin(self, client, admin_headers):
        response = client.post(
            "/api/v1/auth/register",
            data={"email": "new@example.com", "password": "password123"},
            content_type="application/json",
            **admin_headers,
        )

        assert response.status_code == 200
        body = response.json()
        assert body["email"] == "new@example.com"
        assert body["is_active"] is False
        assert User.objects.filter(email="new@example.com", is_active=False).exists()

    def test_conflicts_on_duplicate_email(self, client, admin_headers):
        auth_service.register_user("dup@example.com", "password123")

        response = client.post(
            "/api/v1/auth/register",
            data={"email": "dup@example.com", "password": "password123"},
            content_type="application/json",
            **admin_headers,
        )

        assert response.status_code == 409

    def test_rejects_short_password(self, client, admin_headers):
        response = client.post(
            "/api/v1/auth/register",
            data={"email": "new@example.com", "password": "short"},
            content_type="application/json",
            **admin_headers,
        )

        assert response.status_code == 422


class TestTokenEndpoint:
    def test_issues_token_for_valid_credentials(self, client):
        user = auth_service.register_user("user@example.com", "password123")
        user.is_active = True
        user.save()

        response = client.post(
            "/api/v1/auth/token",
            data={"username": "user@example.com", "password": "password123"},
            content_type="application/json",
        )

        assert response.status_code == 200
        assert response.json()["token_type"] == "bearer"
        assert response.json()["access_token"]

    def test_rejects_invalid_credentials(self, client):
        auth_service.register_user("user@example.com", "password123")

        response = client.post(
            "/api/v1/auth/token",
            data={"username": "user@example.com", "password": "wrong"},
            content_type="application/json",
        )

        assert response.status_code == 401


class TestMeEndpoint:
    def test_returns_current_user(self, client):
        user = auth_service.register_user("user@example.com", "password123")
        user.is_active = True
        user.save()
        token = create_access_token(subject=str(user.id), token_type="user")

        response = client.get("/api/v1/auth/me", HTTP_AUTHORIZATION=f"Bearer {token}")

        assert response.status_code == 200
        assert response.json()["email"] == "user@example.com"

    def test_requires_auth(self, client):
        response = client.get("/api/v1/auth/me")
        assert response.status_code == 401


class TestServiceClientEndpoints:
    def test_create_client_requires_admin(self, client):
        response = client.post(
            "/api/v1/auth/s2s/clients",
            data={"name": "billing-service"},
            content_type="application/json",
        )
        assert response.status_code == 401

    def test_create_client_returns_usable_api_key(self, client, admin_headers):
        response = client.post(
            "/api/v1/auth/s2s/clients",
            data={"name": "billing-service"},
            content_type="application/json",
            **admin_headers,
        )

        assert response.status_code == 200
        body = response.json()
        assert body["name"] == "billing-service"
        assert body["encryption_key"]
        api_key = body["api_key"]

        ping_response = client.get("/api/v1/auth/s2s/ping", HTTP_X_API_KEY=api_key)
        assert ping_response.status_code == 200
        assert ping_response.json()["identity"]["name"] == "billing-service"

    def test_create_client_conflicts_on_duplicate_name(self, client, admin_headers):
        auth_service.create_service_client("billing-service")

        response = client.post(
            "/api/v1/auth/s2s/clients",
            data={"name": "billing-service"},
            content_type="application/json",
            **admin_headers,
        )

        assert response.status_code == 409

    def test_s2s_ping_rejects_invalid_api_key(self, client):
        response = client.get("/api/v1/auth/s2s/ping", HTTP_X_API_KEY="sk_live_bad.wrong")
        assert response.status_code == 401
