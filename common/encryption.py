"""
Encryption utilities for securing configs and secrets
"""
from cryptography.fernet import Fernet
from cryptography.hazmat.primitives import hashes
from cryptography.hazmat.primitives.kdf.pbkdf2 import PBKDF2HMAC
from cryptography.hazmat.backends import default_backend
import base64
import os


def generate_encryption_key() -> str:
    """
    Generate a new Fernet encryption key
    Returns base64-encoded key as string
    """
    key = Fernet.generate_key()
    return key.decode('utf-8')


def derive_key_from_password(password: str, salt: bytes = None) -> tuple[bytes, bytes]:
    """
    Derive an encryption key from a password using PBKDF2
    Returns (key, salt) tuple
    """
    if salt is None:
        salt = os.urandom(16)
    
    kdf = PBKDF2HMAC(
        algorithm=hashes.SHA256(),
        length=32,
        salt=salt,
        iterations=100000,
        backend=default_backend()
    )
    key = base64.urlsafe_b64encode(kdf.derive(password.encode()))
    return key, salt


def encrypt_value(value: str, encryption_key: str) -> str:
    """
    Encrypt a value using the provided encryption key
    
    Args:
        value: Plain text value to encrypt
        encryption_key: Base64-encoded Fernet key
    
    Returns:
        Base64-encoded encrypted value
    """
    if not value:
        return value
    
    fernet = Fernet(encryption_key.encode('utf-8'))
    encrypted = fernet.encrypt(value.encode('utf-8'))
    return encrypted.decode('utf-8')


def decrypt_value(encrypted_value: str, encryption_key: str) -> str:
    """
    Decrypt a value using the provided encryption key
    
    Args:
        encrypted_value: Base64-encoded encrypted value
        encryption_key: Base64-encoded Fernet key
    
    Returns:
        Decrypted plain text value
    """
    if not encrypted_value:
        return encrypted_value
    
    fernet = Fernet(encryption_key.encode('utf-8'))
    decrypted = fernet.decrypt(encrypted_value.encode('utf-8'))
    return decrypted.decode('utf-8')


class EncryptionService:
    """Service for handling encryption operations"""
    
    @staticmethod
    def encrypt_for_storage(value: str, master_key: str = None) -> str:
        """
        Encrypt value for storage in database using master key
        
        Args:
            value: Plain text value
            master_key: Master encryption key (from settings)
        
        Returns:
            Encrypted value
        """
        if master_key is None:
            from django.conf import settings
            master_key = settings.MASTER_ENCRYPTION_KEY
        
        return encrypt_value(value, master_key)
    
    @staticmethod
    def decrypt_from_storage(encrypted_value: str, master_key: str = None) -> str:
        """
        Decrypt value from database storage using master key
        
        Args:
            encrypted_value: Encrypted value from DB
            master_key: Master encryption key (from settings)
        
        Returns:
            Decrypted plain text value
        """
        if master_key is None:
            from django.conf import settings
            master_key = settings.MASTER_ENCRYPTION_KEY
        
        return decrypt_value(encrypted_value, master_key)
    
    @staticmethod
    def re_encrypt_for_client(decrypted_value: str, client_key: str) -> str:
        """
        Re-encrypt a decrypted value using client's encryption key
        
        Args:
            decrypted_value: Plain text value
            client_key: Client's encryption key
        
        Returns:
            Value encrypted with client key
        """
        return encrypt_value(decrypted_value, client_key)
