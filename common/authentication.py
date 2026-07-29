"""
Django Ninja authentication classes - Simplified
"""
from ninja.security import HttpBearer, APIKeyHeader
from django.http import HttpRequest
from typing import Optional
import uuid

from authentication.models import User, ServiceClient
from authentication.services import AuthService
from common.jwt_utils import decode_access_token


auth_service = AuthService()


class JWTAuth(HttpBearer):
    """JWT Bearer authentication for Django Ninja"""
    
    def authenticate(self, request: HttpRequest, token: str) -> Optional[User]:
        """Authenticate a request using JWT Bearer token"""
        try:
            payload = decode_access_token(token)
            
            if payload.get("type") != "user":
                return None
            
            user_id = payload.get("sub")
            if not user_id:
                return None
            
            return User.objects.get(id=uuid.UUID(user_id), is_active=True)
        except (User.DoesNotExist, ValueError):
            return None

class APIKeyAuth(APIKeyHeader):
    """Service-to-service API Key authentication."""

    param_name = "X-API-Key"

    def authenticate(self, request: HttpRequest, key: str) -> Optional[ServiceClient]: # type: ignore
        """Authenticate a request using the <key_id>.<secret> header format."""
        api_key = request.headers.get(self.param_name) or key
        if not api_key:
            return None

        client = auth_service.authenticate_service_api_key(api_key)
        if client is not None:
            request.service_client = client # type: ignore
            request.auth = client # type: ignore
        return client
