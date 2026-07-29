"""
Security utilities for password hashing and secret generation
"""
import secrets
from django.contrib.auth.hashers import check_password, make_password


def hash_password(password: str) -> str:
    """
    Hash a password using Django's built-in password hashing.

    Args:
        password: Plain text password

    Returns:
        Hashed password
    """
    return make_password(password)


def verify_password(plain_password: str, hashed_password: str) -> bool:
    """
    Verify a password against its hash.

    Args:
        plain_password: Plain text password
        hashed_password: Hashed password

    Returns:
        True if password matches, False otherwise
    """
    return check_password(plain_password, hashed_password)


def generate_random_secret(prefix: str = "secret") -> str:
    """
    Generate a random secret key
    
    Args:
        prefix: Prefix for the secret
    
    Returns:
        Random secret string with prefix
    """
    random_part = secrets.token_urlsafe(32)
    return f"{prefix}-{random_part}"
