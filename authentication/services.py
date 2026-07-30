"""
Authentication services - Simplified
"""
import secrets
import uuid
from dataclasses import dataclass
from typing import Optional

from authentication.models import User, ServiceClient
from common.security import hash_password, verify_password


@dataclass
class ServiceClientCredentials:
    """Service client with API key credentials"""
    client: ServiceClient
    api_key: str


class AuthService:
    """Authentication service for users and service clients"""
    
    def register_user(self, email: str, password: str, username: Optional[str] = None) -> User:
        """Register a new user"""
        if User.objects.filter(email=email).exists():
            raise ValueError("User already exists")
        
        if not username:
            username = email.split('@')[0] + str(uuid.uuid4())[:8]
        
        user = User.objects.create(
            id=uuid.uuid4(),
            email=email,
            username=username,
            is_active=True,
        )
        user.set_password(password)
        user.save()
        return user
    
    def authenticate_user(self, email: str, password: str) -> Optional[User]:
        """Authenticate a user by email and password"""
        try:
            user = User.objects.get(email=email, is_active=True)
            if user.check_password(password):
                return user
        except User.DoesNotExist:
            pass
        return None
    
    def get_user_by_id(self, user_id: str) -> Optional[User]:
        """Get user by ID"""
        try:
            return User.objects.get(id=uuid.UUID(user_id), is_active=True)
        except (User.DoesNotExist, ValueError):
            return None
    
    def create_service_client(self, name: str) -> ServiceClientCredentials:
        """Create a new service client with API key and encryption key"""
        from common.encryption import generate_encryption_key
        
        if ServiceClient.objects.filter(name=name).exists():
            raise ValueError("Service client already exists")
        
        api_key_id = f"sk_live_{secrets.token_hex(4)}"
        api_key_secret = secrets.token_urlsafe(24)
        raw_api_key = f"{api_key_id}.{api_key_secret}"
        client_encryption_key = generate_encryption_key()
        
        client = ServiceClient.objects.create(
            id=uuid.uuid4(),
            name=name,
            api_key_id=api_key_id,
            api_key_hash=hash_password(api_key_secret),
            encryption_key=client_encryption_key,
            is_active=True,
        )
        return ServiceClientCredentials(client=client, api_key=raw_api_key)
    
    def authenticate_service_api_key(self, api_key: str) -> Optional[ServiceClient]:
        """Authenticate a service client using the <key_id>.<secret> format"""
        if not api_key:
            return None

        if "." not in api_key:
            return None

        key_id, secret = api_key.split('.', 1)
        if not key_id or not secret:
            return None

        client = ServiceClient.objects.filter(is_active=True, api_key_id=key_id).first()
        if client and client.api_key_hash and verify_password(secret, client.api_key_hash):
            return client
        return None
    
    def get_service_client_by_id(self, client_id: str) -> Optional[ServiceClient]:
        """Get service client by ID"""
        try:
            return ServiceClient.objects.get(id=uuid.UUID(client_id), is_active=True)
        except (ServiceClient.DoesNotExist, ValueError):
            return None
