"""
Integration tests for common/activity_logger.py - uses the sqlite test database.
"""
from unittest.mock import patch

import pytest
from django.test import RequestFactory

from common.activity_logger import (
    get_client_ip,
    log_activity,
    log_auth_failed,
    log_create,
    log_login,
    log_login_failed,
    log_logout,
)
from config_management.models import Activity

pytestmark = pytest.mark.django_db


@pytest.fixture
def request_factory():
    return RequestFactory()


class TestGetClientIp:
    def test_prefers_x_forwarded_for_header(self, request_factory):
        request = request_factory.get("/", HTTP_X_FORWARDED_FOR="203.0.113.5, 10.0.0.1")
        assert get_client_ip(request) == "203.0.113.5"

    def test_falls_back_to_remote_addr(self, request_factory):
        request = request_factory.get("/", REMOTE_ADDR="127.0.0.1")
        assert get_client_ip(request) == "127.0.0.1"


class TestLogActivity:
    def test_creates_activity_row_with_expected_fields(self, request_factory):
        request = request_factory.get("/", REMOTE_ADDR="10.1.1.1")

        log_create("config", "cfg-id", "myapp/prod/key", request=request, details={"is_secret": True})

        activity = Activity.objects.get()
        assert activity.type == "create"
        assert activity.resource == "config"
        assert activity.resource_id == "cfg-id"
        assert activity.resource_name == "myapp/prod/key"
        assert activity.ip_address == "10.1.1.1"
        assert activity.details == '{"is_secret": true}'

    def test_pulls_user_email_from_authenticated_request_when_not_given(self, request_factory, django_user_model):
        user = django_user_model.objects.create(email="alice@example.com", username="alice")
        request = request_factory.get("/")
        request.user = user

        log_activity("update", "config", "cfg-id", request=request)

        activity = Activity.objects.get()
        assert activity.user_email == "alice@example.com"

    def test_explicit_user_email_wins_over_request_user(self, request_factory, django_user_model):
        user = django_user_model.objects.create(email="alice@example.com", username="alice")
        request = request_factory.get("/")
        request.user = user

        log_activity("update", "config", "cfg-id", user_email="explicit@example.com", request=request)

        activity = Activity.objects.get()
        assert activity.user_email == "explicit@example.com"

    def test_non_serializable_details_falls_back_to_str(self, request_factory):
        request = request_factory.get("/")

        log_activity("update", "config", "cfg-id", request=request, details=object())

        activity = Activity.objects.get()
        assert activity.details.startswith("<object object")

    def test_failure_to_write_activity_is_swallowed_not_raised(self):
        with patch("config_management.models.Activity.objects.create", side_effect=Exception("db down")):
            # Must not raise - an audit failure should never break the caller's request.
            log_activity("create", "config", "cfg-id")

        assert Activity.objects.count() == 0


class TestConvenienceHelpers:
    def test_log_login_sets_user_email_and_resource(self):
        log_login("bob@example.com", details="Web UI login")

        activity = Activity.objects.get()
        assert activity.type == "login"
        assert activity.resource == "user"
        assert activity.user_email == "bob@example.com"

    def test_log_logout_sets_expected_fields(self):
        log_logout("bob@example.com")

        activity = Activity.objects.get()
        assert activity.type == "logout"
        assert activity.user_email == "bob@example.com"

    def test_log_login_failed_records_attempted_identifier(self):
        log_login_failed("unknown@example.com", details="bad password")

        activity = Activity.objects.get()
        assert activity.type == "login_failed"
        assert activity.resource_id == "unknown@example.com"

    def test_log_auth_failed_records_non_user_resource(self):
        log_auth_failed("client", "sk_live_deadbeef")

        activity = Activity.objects.get()
        assert activity.type == "auth_failed"
        assert activity.resource == "client"
        assert activity.resource_id == "sk_live_deadbeef"
