"""
Integration tests for the Ninja endpoints in config_management/api.py,
exercised through the real HTTP router against the sqlite test DB.
"""
import pytest

from authentication.models import User
from authentication.services import AuthService
from common.encryption import decrypt_value
from common.jwt_utils import create_access_token
from config_management.services import ConfigService

pytestmark = pytest.mark.django_db

auth_service = AuthService()
config_service = ConfigService()


@pytest.fixture
def user():
    u = User.objects.create(email="user@example.com", username="user", is_active=True)
    u.set_password("password123")
    u.save()
    return u


@pytest.fixture
def user_headers(user):
    token = create_access_token(subject=str(user.id), token_type="user")
    return {"HTTP_AUTHORIZATION": f"Bearer {token}"}


@pytest.fixture
def service_client_credentials():
    return auth_service.create_service_client("billing-service")


class TestUpsertConfigEndpoint:
    def test_requires_jwt_auth(self, client):
        response = client.post(
            "/api/v1/config/configs/upsert",
            data={"service": "payments", "environment": "prod", "key": "API_URL", "value": "https://a"},
            content_type="application/json",
        )
        assert response.status_code == 401

    def test_creates_config_and_never_returns_raw_value(self, client, user_headers):
        response = client.post(
            "/api/v1/config/configs/upsert",
            data={
                "service": "payments",
                "environment": "prod",
                "key": "API_URL",
                "value": "https://api.example.com",
                "is_secret": True,
            },
            content_type="application/json",
            **user_headers,
        )

        assert response.status_code == 200
        body = response.json()
        assert body["value"] == "***ENCRYPTED***"
        assert body["is_secret"] is True

        stored = config_service.get_config("payments", "prod", "API_URL")
        assert config_service.decrypt_config_value(stored) == "https://api.example.com"

    def test_upserting_same_key_updates_rather_than_duplicates(self, client, user_headers):
        payload = {"service": "payments", "environment": "prod", "key": "API_URL", "value": "https://v1"}
        client.post("/api/v1/config/configs/upsert", data=payload, content_type="application/json", **user_headers)

        payload["value"] = "https://v2"
        client.post("/api/v1/config/configs/upsert", data=payload, content_type="application/json", **user_headers)

        stored = config_service.get_config("payments", "prod", "API_URL")
        assert config_service.decrypt_config_value(stored) == "https://v2"


class TestConfigHistoryAndRollbackEndpoints:
    def test_history_requires_jwt_auth(self, client):
        entry = config_service.upsert_config("payments", "prod", "API_URL", "https://v1")
        response = client.get(f"/api/v1/config/configs/{entry.id}/history")
        assert response.status_code == 401

    def test_history_lists_versions_newest_first_with_decrypted_values(self, client, user_headers):
        entry = config_service.upsert_config("payments", "prod", "API_URL", "https://v1")
        config_service.upsert_config("payments", "prod", "API_URL", "https://v2")

        response = client.get(f"/api/v1/config/configs/{entry.id}/history", **user_headers)

        assert response.status_code == 200
        body = response.json()
        assert [v["action"] for v in body] == ["update", "create"]
        assert body[0]["value"] == "https://v2"
        assert body[1]["value"] == "https://v1"

    def test_history_never_exposes_secret_values(self, client, user_headers):
        entry = config_service.upsert_config("payments", "prod", "DB_PASSWORD", "s3cret", is_secret=True)

        response = client.get(f"/api/v1/config/configs/{entry.id}/history", **user_headers)

        body = response.json()
        assert body[0]["is_secret"] is True
        assert body[0]["value"] is None

    def test_rollback_restores_prior_value(self, client, user_headers):
        entry = config_service.upsert_config("payments", "prod", "API_URL", "https://v1")
        config_service.upsert_config("payments", "prod", "API_URL", "https://v2")

        response = client.post(
            f"/api/v1/config/configs/{entry.id}/rollback",
            data={"version": 1},
            content_type="application/json",
            **user_headers,
        )

        assert response.status_code == 200
        assert response.json()["value"] == "***ENCRYPTED***"

        stored = config_service.get_config("payments", "prod", "API_URL")
        assert config_service.decrypt_config_value(stored) == "https://v1"

    def test_rollback_to_unknown_version_returns_404(self, client, user_headers):
        entry = config_service.upsert_config("payments", "prod", "API_URL", "https://v1")

        response = client.post(
            f"/api/v1/config/configs/{entry.id}/rollback",
            data={"version": 99},
            content_type="application/json",
            **user_headers,
        )

        assert response.status_code == 404

    def test_rollback_requires_jwt_auth(self, client):
        entry = config_service.upsert_config("payments", "prod", "API_URL", "https://v1")
        response = client.post(
            f"/api/v1/config/configs/{entry.id}/rollback",
            data={"version": 1},
            content_type="application/json",
        )
        assert response.status_code == 401


class TestListConfigsForClientEndpoint:
    def test_requires_api_key_auth(self, client):
        response = client.get("/api/v1/config/configs/list", {"service": "payments", "environment": "prod"})
        assert response.status_code == 401

    def test_returns_configs_encrypted_with_client_key(self, client, service_client_credentials):
        config_service.upsert_config("payments", "prod", "API_URL", "https://api.example.com")

        response = client.get(
            "/api/v1/config/configs/list",
            {"service": "payments", "environment": "prod"},
            HTTP_X_API_KEY=service_client_credentials.api_key,
        )

        assert response.status_code == 200
        body = response.json()
        assert len(body) == 1
        assert body[0]["key"] == "API_URL"
        decrypted = decrypt_value(body[0]["value"], service_client_credentials.client.encryption_key)
        assert decrypted == "https://api.example.com"

    def test_returns_empty_list_for_unknown_scope(self, client, service_client_credentials):
        response = client.get(
            "/api/v1/config/configs/list",
            {"service": "unknown", "environment": "prod"},
            HTTP_X_API_KEY=service_client_credentials.api_key,
        )

        assert response.status_code == 200
        assert response.json() == []


class TestFeatureFlagEndpoints:
    def test_create_flag_creates_across_all_environments(self, client, user_headers):
        config_service.upsert_config("payments", "prod", "seed", "v")
        config_service.upsert_config("payments", "staging", "seed", "v")

        response = client.post(
            "/api/v1/config/feature-flags",
            data={"service": "payments", "name": "new-checkout", "is_enabled": True},
            content_type="application/json",
            **user_headers,
        )

        assert response.status_code == 200
        assert response.json()["created_count"] == 2

    def test_create_flag_conflicts_when_application_unknown(self, client, user_headers):
        response = client.post(
            "/api/v1/config/feature-flags",
            data={"service": "unknown-app", "name": "new-checkout"},
            content_type="application/json",
            **user_headers,
        )

        assert response.status_code == 409

    def test_list_and_toggle_flag_roundtrip(self, client, user_headers):
        config_service.upsert_config("payments", "prod", "seed", "v")
        client.post(
            "/api/v1/config/feature-flags",
            data={"service": "payments", "environment": "prod", "name": "new-checkout", "is_enabled": False},
            content_type="application/json",
            **user_headers,
        )

        list_response = client.get(
            "/api/v1/config/feature-flags", {"service": "payments", "environment": "prod"}, **user_headers
        )
        assert list_response.status_code == 200
        assert list_response.json()[0]["is_enabled"] is False

        toggle_response = client.post(
            "/api/v1/config/feature-flags/new-checkout/toggle?service=payments&environment=prod",
            **user_headers,
        )
        assert toggle_response.status_code == 200
        assert toggle_response.json()["is_enabled"] is True

    def test_toggle_returns_404_for_unknown_flag(self, client, user_headers):
        response = client.post(
            "/api/v1/config/feature-flags/missing/toggle?service=payments&environment=prod",
            **user_headers,
        )
        assert response.status_code == 404

    def test_list_accepts_service_client_api_key(self, client, user_headers, service_client_credentials):
        config_service.upsert_config("payments", "prod", "seed", "v")
        client.post(
            "/api/v1/config/feature-flags",
            data={"service": "payments", "environment": "prod", "name": "new-checkout", "is_enabled": True},
            content_type="application/json",
            **user_headers,
        )

        response = client.get(
            "/api/v1/config/feature-flags",
            {"service": "payments", "environment": "prod"},
            HTTP_X_API_KEY=service_client_credentials.api_key,
        )

        assert response.status_code == 200
        assert response.json()[0]["name"] == "new-checkout"

    def test_list_rejects_missing_auth(self, client):
        response = client.get(
            "/api/v1/config/feature-flags", {"service": "payments", "environment": "prod"}
        )
        assert response.status_code == 401
