"""
Django Ninja authentication classes - Simplified
"""
import logging
from ninja.security import HttpBearer, APIKeyHeader
from django.http import HttpRequest
from typing import Optional
import uuid

from authentication.models import User, ServiceClient
from authentication.services import AuthService
from common.jwt_utils import decode_access_token

logger = logging.getLogger(__name__)

auth_service = AuthService()


class JWTAuth(HttpBearer):
    """JWT Bearer authentication for Django Ninja"""

    def authenticate(self, request: HttpRequest, token: str) -> Optional[User]:
        """Authenticate a request using JWT Bearer token"""
        try:
            payload = decode_access_token(token)

            if payload.get("type") != "user":
                logger.warning("JWT auth rejected: wrong token type on %s", request.path)
                return None

            user_id = payload.get("sub")
            if not user_id:
                logger.warning("JWT auth rejected: missing subject claim on %s", request.path)
                return None

            return User.objects.get(id=uuid.UUID(user_id), is_active=True)
        except User.DoesNotExist:
            logger.warning("JWT auth rejected: user not found or inactive on %s", request.path)
            return None
        except ValueError as e:
            logger.warning("JWT auth rejected on %s: %s", request.path, e)
            return None


class JWTAdminAuth(JWTAuth):
    """JWT authentication restricted to admin/staff users."""

    def authenticate(self, request: HttpRequest, token: str) -> Optional[User]:
        user = super().authenticate(request, token)
        if user is None:
            return None
        if not user.is_staff:
            logger.warning("JWT admin auth rejected: user %s is not staff (%s)", user.email, request.path)
            return None
        return user


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
            logger.debug("API key auth succeeded for client %s on %s", client.name, request.path)
            request.service_client = client # type: ignore
            request.auth = client # type: ignore
        else:
            key_id = api_key.split(".", 1)[0] if "." in api_key else "<malformed>"
            logger.warning("API key auth rejected for key_id=%s on %s", key_id, request.path)
        return client
