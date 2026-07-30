"""
Integration tests for Django Ninja auth classes in common/authentication.py.
Uses the sqlite test database (see control_plane_project/test_settings.py).
"""
import uuid

import pytest
from django.test import RequestFactory

from authentication.models import ServiceClient, User
from authentication.services import AuthService
from common.authentication import APIKeyAuth, JWTAdminAuth, JWTAuth
from common.jwt_utils import create_access_token

pytestmark = pytest.mark.django_db

auth_service = AuthService()


@pytest.fixture
def request_factory():
    return RequestFactory()


@pytest.fixture
def active_user():
    user = User.objects.create(email="member@example.com", username="member")
    user.set_password("password123")
    user.save()
    return user


@pytest.fixture
def staff_user():
    user = User.objects.create(email="admin@example.com", username="admin", is_staff=True)
    user.set_password("password123")
    user.save()
    return user


class TestJWTAuth:
    def test_authenticate_accepts_valid_user_token(self, request_factory, active_user):
        token = create_access_token(subject=str(active_user.id), token_type="user")
        request = request_factory.get("/api/auth/me")

        result = JWTAuth().authenticate(request, token)

        assert result == active_user

    def test_authenticate_rejects_service_token_type(self, request_factory, active_user):
        token = create_access_token(subject=str(active_user.id), token_type="service")
        request = request_factory.get("/api/auth/me")

        assert JWTAuth().authenticate(request, token) is None

    def test_authenticate_rejects_token_for_missing_user(self, request_factory):
        token = create_access_token(subject=str(uuid.uuid4()), token_type="user")
        request = request_factory.get("/api/auth/me")

        assert JWTAuth().authenticate(request, token) is None

    def test_authenticate_rejects_token_for_inactive_user(self, request_factory, active_user):
        active_user.is_active = False
        active_user.save()
        token = create_access_token(subject=str(active_user.id), token_type="user")
        request = request_factory.get("/api/auth/me")

        assert JWTAuth().authenticate(request, token) is None

    def test_authenticate_rejects_malformed_token(self, request_factory):
        request = request_factory.get("/api/auth/me")
        assert JWTAuth().authenticate(request, "garbage") is None


class TestJWTAdminAuth:
    def test_authenticate_accepts_staff_user(self, request_factory, staff_user):
        token = create_access_token(subject=str(staff_user.id), token_type="user")
        request = request_factory.get("/api/auth/register")

        assert JWTAdminAuth().authenticate(request, token) == staff_user

    def test_authenticate_rejects_non_staff_user(self, request_factory, active_user):
        token = create_access_token(subject=str(active_user.id), token_type="user")
        request = request_factory.get("/api/auth/register")

        assert JWTAdminAuth().authenticate(request, token) is None


class TestAPIKeyAuth:
    def test_authenticate_accepts_valid_api_key(self, request_factory):
        credentials = auth_service.create_service_client("billing-service")
        request = request_factory.get("/api/config/configs/list")

        client = APIKeyAuth().authenticate(request, credentials.api_key)

        assert client == credentials.client
        assert request.auth == credentials.client
        assert request.service_client == credentials.client

    def test_authenticate_rejects_wrong_secret(self, request_factory):
        credentials = auth_service.create_service_client("billing-service")
        key_id = credentials.api_key.split(".", 1)[0]
        request = request_factory.get("/api/config/configs/list")

        assert APIKeyAuth().authenticate(request, f"{key_id}.wrong-secret") is None

    def test_authenticate_rejects_inactive_client(self, request_factory):
        credentials = auth_service.create_service_client("billing-service")
        credentials.client.is_active = False
        credentials.client.save()
        request = request_factory.get("/api/config/configs/list")

        assert APIKeyAuth().authenticate(request, credentials.api_key) is None

    def test_authenticate_rejects_empty_key(self, request_factory):
        request = request_factory.get("/api/config/configs/list")
        assert APIKeyAuth().authenticate(request, "") is None
