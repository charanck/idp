"""
Unit tests for common/security.py - no database needed.
"""
from common.security import generate_random_secret, hash_password, verify_password


def test_hash_password_does_not_return_plaintext():
    hashed = hash_password("hunter2")
    assert hashed != "hunter2"


def test_verify_password_accepts_correct_password():
    hashed = hash_password("hunter2")
    assert verify_password("hunter2", hashed) is True


def test_verify_password_rejects_incorrect_password():
    hashed = hash_password("hunter2")
    assert verify_password("wrong-password", hashed) is False


def test_generate_random_secret_uses_given_prefix():
    secret = generate_random_secret(prefix="sk_live")
    assert secret.startswith("sk_live-")


def test_generate_random_secret_defaults_prefix():
    secret = generate_random_secret()
    assert secret.startswith("secret-")


def test_generate_random_secret_is_unique_per_call():
    assert generate_random_secret() != generate_random_secret()
