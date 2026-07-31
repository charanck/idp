"""
Authentication API endpoints
"""
import logging

from ninja import Router
from ninja.errors import HttpError
from django.http import HttpRequest

from authentication.schemas import (
    RegisterUserRequest,
    UserResponse,
    TokenRequest,
    TokenResponse,
    CreateServiceClientRequest,
    ServiceClientResponse,
)
from authentication.services import AuthService
from common.activity_logger import log_create, log_login, log_login_failed
from common.jwt_utils import create_access_token
from common.authentication import JWTAuth, JWTAdminAuth, APIKeyAuth

logger = logging.getLogger(__name__)

router = Router()
auth_service = AuthService()

# JWT authentication instance
jwt_auth = JWTAuth()
admin_jwt_auth = JWTAdminAuth()
# API Key authentication instance
api_key_auth = APIKeyAuth()


@router.post("/register", response=UserResponse, auth=admin_jwt_auth, tags=["auth"])
def register_user(request: HttpRequest, payload: RegisterUserRequest):
    """Register a new user"""
    try:
        user = auth_service.register_user(payload.email, payload.password)
    except ValueError as e:
        raise HttpError(409, str(e))
    except Exception:
        logger.exception("Unexpected error registering user %s", payload.email)
        raise HttpError(500, "Failed to register user")

    log_create('user', str(user.id), user.email, request=request, details={'created_by_api': True})
    return UserResponse(
        id=str(user.id),
        email=user.email,
        username=user.username,
        is_active=user.is_active
    )


@router.post("/token", response=TokenResponse, tags=["auth"])
def create_user_token(request: HttpRequest, payload: TokenRequest):
    """Create JWT token for user"""
    user = auth_service.authenticate_user(payload.username, payload.password)
    if user is None:
        log_login_failed(payload.username, request=request, details='Invalid API token request')
        raise HttpError(401, "Invalid email or password")

    token = create_access_token(subject=str(user.id), token_type="user")
    log_login(user.email, request=request, details='API token issued')
    return TokenResponse(access_token=token)


@router.get("/me", response=UserResponse, auth=jwt_auth, tags=["auth"])
def get_current_user_info(request: HttpRequest):
    """Get current authenticated user info"""
    user = request.auth
    return UserResponse(
        id=str(user.id),
        email=user.email,
        username=user.username,
        is_active=user.is_active
    )


@router.post("/s2s/clients", response=ServiceClientResponse, auth=admin_jwt_auth, tags=["s2s"])
def create_service_client(request: HttpRequest, payload: CreateServiceClientRequest):
    """Create a new service client for S2S authentication"""
    try:
        credentials = auth_service.create_service_client(payload.name)
    except ValueError as e:
        raise HttpError(409, str(e))
    except Exception:
        logger.exception("Unexpected error creating service client %s", payload.name)
        raise HttpError(500, "Failed to create service client")

    log_create(
        'client',
        str(credentials.client.id),
        credentials.client.name,
        request=request,
        details={'created_by_api': True, 'api_key_id': credentials.client.api_key_id},
    )
    return ServiceClientResponse(
        id=str(credentials.client.id),
        name=credentials.client.name,
        client_id=str(credentials.client.id),
        api_key_id=credentials.client.api_key_id or '',
        api_key=credentials.api_key,
        encryption_key=credentials.client.encryption_key,
    )


@router.get("/s2s/ping", auth=api_key_auth, tags=["s2s"])
def s2s_ping(request: HttpRequest):
    """S2S authentication ping endpoint"""
    client = request.auth
    logger.debug("S2S ping from client %s", client.name)
    return {
        "ok": True,
        "identity": {
            "method": "api_key",
            "client_id": str(client.id),
            "name": client.name
        }
    }
