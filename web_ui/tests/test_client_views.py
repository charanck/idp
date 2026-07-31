"""
Integration tests for service-client and activity-log web UI views, exercised
through the real HTTP client (session auth) against the sqlite test DB.
"""
import pytest
from django.test import Client

from authentication.models import ServiceClient, User
from authentication.services import AuthService
from config_management.models import Activity

pytestmark = pytest.mark.django_db

auth_service = AuthService()


@pytest.fixture
def admin_user():
    user = User.objects.create(email="admin@example.com", username="admin", is_staff=True)
    user.set_password("password123")
    user.save()
    return user


@pytest.fixture
def admin_client(admin_user):
    client = Client()
    client.force_login(admin_user)
    return client


@pytest.fixture
def service_client():
    return auth_service.create_service_client("billing-service").client


class TestClientDelete:
    def test_requires_admin(self, client, service_client):
        response = client.post(f"/clients/{service_client.id}/delete/")
        assert response.status_code == 302
        assert ServiceClient.objects.filter(id=service_client.id).exists()

    def test_get_shows_confirmation_page_without_deleting(self, admin_client, service_client):
        response = admin_client.get(f"/clients/{service_client.id}/delete/")
        assert response.status_code == 200
        assert ServiceClient.objects.filter(id=service_client.id).exists()

    def test_post_deletes_client(self, admin_client, service_client):
        response = admin_client.post(f"/clients/{service_client.id}/delete/")
        assert response.status_code == 302
        assert not ServiceClient.objects.filter(id=service_client.id).exists()


class TestClientRegenerateKey:
    def test_requires_admin(self, client, service_client):
        original_key = service_client.encryption_key
        client.post(f"/clients/{service_client.id}/regenerate-key/")
        service_client.refresh_from_db()
        assert service_client.encryption_key == original_key

    def test_post_issues_a_new_key(self, admin_client, service_client):
        original_key = service_client.encryption_key
        response = admin_client.post(f"/clients/{service_client.id}/regenerate-key/")

        service_client.refresh_from_db()
        assert response.status_code == 302
        assert service_client.encryption_key != original_key
        assert service_client.encryption_key != ""

    def test_get_does_not_change_the_key(self, admin_client, service_client):
        original_key = service_client.encryption_key
        admin_client.get(f"/clients/{service_client.id}/regenerate-key/")
        service_client.refresh_from_db()
        assert service_client.encryption_key == original_key


class TestActivityLogFilters:
    def test_resource_and_type_filter_options_are_deduplicated(self, admin_client):
        Activity.objects.create(type="create", resource="config", resource_id="1")
        Activity.objects.create(type="update", resource="config", resource_id="1")
        Activity.objects.create(type="delete", resource="user", resource_id="2")

        response = admin_client.get("/activity/")

        resource_types = list(response.context["resource_types"])
        action_types = list(response.context["action_types"])
        assert sorted(resource_types) == sorted(set(resource_types))
        assert sorted(action_types) == sorted(set(action_types))
        assert "config" in resource_types and "user" in resource_types
