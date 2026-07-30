"""
Unit tests for common/jwt_utils.py - no database needed.
"""
from datetime import datetime, timedelta, timezone

import jwt
import pytest

from common.jwt_utils import create_access_token, decode_access_token


def test_create_and_decode_roundtrip():
    token = create_access_token(subject="user-123", token_type="user")

    payload = decode_access_token(token)

    assert payload["sub"] == "user-123"
    assert payload["type"] == "user"
    assert "exp" in payload
    assert "iat" in payload


def test_create_access_token_defaults_to_user_type():
    token = create_access_token(subject="user-123")
    payload = decode_access_token(token)
    assert payload["type"] == "user"


def test_create_access_token_supports_service_type():
    token = create_access_token(subject="client-456", token_type="service")
    payload = decode_access_token(token)
    assert payload["type"] == "service"


def test_decode_access_token_rejects_expired_token(settings):
    expired_payload = {
        "sub": "user-123",
        "type": "user",
        "exp": datetime.now(timezone.utc) - timedelta(minutes=1),
        "iat": datetime.now(timezone.utc) - timedelta(minutes=5),
    }
    expired_token = jwt.encode(expired_payload, settings.JWT_SECRET_KEY, algorithm=settings.JWT_ALGORITHM)

    with pytest.raises(ValueError, match="expired"):
        decode_access_token(expired_token)


def test_decode_access_token_rejects_garbage_token():
    with pytest.raises(ValueError, match="Invalid token"):
        decode_access_token("not-a-real-token")


def test_decode_access_token_rejects_token_signed_with_wrong_secret(settings):
    payload = {
        "sub": "user-123",
        "type": "user",
        "exp": datetime.now(timezone.utc) + timedelta(minutes=5),
        "iat": datetime.now(timezone.utc),
    }
    token = jwt.encode(payload, "some-other-secret", algorithm=settings.JWT_ALGORITHM)

    with pytest.raises(ValueError, match="Invalid token"):
        decode_access_token(token)
