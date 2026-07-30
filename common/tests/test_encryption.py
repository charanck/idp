"""
Unit tests for common/encryption.py - no database needed.
"""
import pytest
from cryptography.fernet import Fernet, InvalidToken

from common.encryption import (
    EncryptionService,
    decrypt_value,
    derive_key_from_password,
    encrypt_value,
    generate_encryption_key,
)


def test_generate_encryption_key_is_usable_fernet_key():
    key = generate_encryption_key()
    # Should not raise - proves it's a well-formed Fernet key.
    Fernet(key.encode('utf-8'))


def test_generate_encryption_key_returns_unique_keys():
    assert generate_encryption_key() != generate_encryption_key()


def test_encrypt_decrypt_roundtrip():
    key = generate_encryption_key()
    plaintext = "super-secret-value"

    encrypted = encrypt_value(plaintext, key)

    assert encrypted != plaintext
    assert decrypt_value(encrypted, key) == plaintext


def test_encrypt_value_passes_through_empty_string():
    key = generate_encryption_key()
    assert encrypt_value("", key) == ""


def test_decrypt_value_passes_through_empty_string():
    key = generate_encryption_key()
    assert decrypt_value("", key) == ""


def test_decrypt_with_wrong_key_raises_invalid_token():
    key_a = generate_encryption_key()
    key_b = generate_encryption_key()
    encrypted = encrypt_value("data", key_a)

    with pytest.raises(InvalidToken):
        decrypt_value(encrypted, key_b)


def test_derive_key_from_password_is_deterministic_given_same_salt():
    key1, salt = derive_key_from_password("hunter2")
    key2, _ = derive_key_from_password("hunter2", salt=salt)

    assert key1 == key2


def test_derive_key_from_password_differs_with_random_salt():
    key1, salt1 = derive_key_from_password("hunter2")
    key2, salt2 = derive_key_from_password("hunter2")

    assert salt1 != salt2
    assert key1 != key2


class TestEncryptionService:
    def test_encrypt_for_storage_uses_explicit_master_key_and_round_trips(self):
        master_key = generate_encryption_key()
        encrypted = EncryptionService.encrypt_for_storage("db-value", master_key=master_key)

        assert EncryptionService.decrypt_from_storage(encrypted, master_key=master_key) == "db-value"

    def test_encrypt_for_storage_falls_back_to_settings_master_key(self, settings):
        settings.MASTER_ENCRYPTION_KEY = generate_encryption_key()

        encrypted = EncryptionService.encrypt_for_storage("db-value")

        assert EncryptionService.decrypt_from_storage(encrypted) == "db-value"

    def test_re_encrypt_for_client_produces_value_decryptable_with_client_key(self):
        client_key = generate_encryption_key()

        client_encrypted = EncryptionService.re_encrypt_for_client("plain-value", client_key)

        assert decrypt_value(client_encrypted, client_key) == "plain-value"

    def test_decrypt_from_storage_with_wrong_master_key_raises(self):
        key_a = generate_encryption_key()
        key_b = generate_encryption_key()
        encrypted = EncryptionService.encrypt_for_storage("value", master_key=key_a)

        with pytest.raises(InvalidToken):
            EncryptionService.decrypt_from_storage(encrypted, master_key=key_b)
