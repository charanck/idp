"""
Integration tests for the auth rate-limit middleware - uses the sqlite test
database and a real Django test Client so the full middleware stack runs.
"""
import pytest
from django.core.cache import caches
from django.test import Client

pytestmark = pytest.mark.django_db


@pytest.fixture(autouse=True)
def clear_ratelimit_cache():
    caches['ratelimit'].clear()
    yield
    caches['ratelimit'].clear()


@pytest.fixture(autouse=True)
def low_rate_limit(settings):
    settings.AUTH_RATE_LIMIT = 2
    settings.AUTH_RATE_LIMIT_WINDOW_SECONDS = 60


class TestRateLimitMiddleware:
    def test_allows_requests_under_the_limit(self):
        client = Client()

        for _ in range(2):
            response = client.post(
                "/api/v1/auth/register",
                data={"email": "user@example.com", "password": "not-long-enough"},
                content_type="application/json",
            )
            assert response.status_code != 429

    def test_blocks_requests_over_the_limit(self):
        client = Client()

        for _ in range(2):
            client.post(
                "/api/v1/auth/register",
                data={"email": "user@example.com", "password": "not-long-enough"},
                content_type="application/json",
            )

        response = client.post(
            "/api/v1/auth/register",
            data={"email": "user@example.com", "password": "not-long-enough"},
            content_type="application/json",
        )

        assert response.status_code == 429
        assert "Retry-After" in response.headers

    def test_only_configured_paths_are_limited(self):
        client = Client()

        # Well over the limit, but /api/v1/auth/me isn't a limited path.
        for _ in range(5):
            response = client.get("/api/v1/auth/me")
            assert response.status_code != 429

    def test_limits_are_scoped_per_bucket(self):
        client = Client()

        for _ in range(2):
            client.post(
                "/api/v1/auth/register",
                data={"email": "user@example.com", "password": "not-long-enough"},
                content_type="application/json",
            )

        # /login/ is a different bucket, so it should still be allowed.
        response = client.post("/login/", data={"username": "user@example.com", "password": "wrong"})
        assert response.status_code != 429
