"""
JWT Token utilities for authentication
"""
import jwt
from datetime import datetime, timedelta, timezone
from django.conf import settings


def create_access_token(subject: str, token_type: str = "user") -> str:
    """
    Create a JWT access token
    
    Args:
        subject: User ID or service client ID
        token_type: Type of token ('user' or 'service')
    
    Returns:
        JWT token string
    """
    expire = datetime.now(timezone.utc) + timedelta(minutes=settings.JWT_EXPIRE_MINUTES)
    
    payload = {
        "sub": subject,
        "type": token_type,
        "exp": expire,
        "iat": datetime.now(timezone.utc),
    }
    
    token = jwt.encode(
        payload,
        settings.JWT_SECRET_KEY,
        algorithm=settings.JWT_ALGORITHM
    )
    
    return token


def decode_access_token(token: str) -> dict:
    """
    Decode and validate a JWT access token
    
    Args:
        token: JWT token string
    
    Returns:
        Decoded token payload
    
    Raises:
        ValueError: If token is invalid or expired
    """
    try:
        payload = jwt.decode(
            token,
            settings.JWT_SECRET_KEY,
            algorithms=[settings.JWT_ALGORITHM]
        )
        return payload
    except jwt.ExpiredSignatureError as e:
        raise ValueError("Token has expired") from e
    except jwt.InvalidTokenError as e:
        raise ValueError("Invalid token") from e
