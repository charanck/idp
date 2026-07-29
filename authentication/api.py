"""
Authentication API endpoints
"""
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
from common.jwt_utils import create_access_token
from common.authentication import JWTAuth, APIKeyAuth

router = Router()
auth_service = AuthService()

# JWT authentication instance
jwt_auth = JWTAuth()
# API Key authentication instance
api_key_auth = APIKeyAuth()


@router.post("/register", response=UserResponse, tags=["auth"])
def register_user(request: HttpRequest, payload: RegisterUserRequest):
    """Register a new user"""
    try:
        user = auth_service.register_user(payload.email, payload.password)
        return UserResponse(
            id=str(user.id),
            email=user.email,
            username=user.username,
            is_active=user.is_active
        )
    except ValueError as e:
        raise HttpError(409, str(e))


@router.post("/token", response=TokenResponse, tags=["auth"])
def create_user_token(request: HttpRequest, payload: TokenRequest):
    """Create JWT token for user"""
    user = auth_service.authenticate_user(payload.username, payload.password)
    if user is None:
        raise HttpError(401, "Invalid email or password")
    
    token = create_access_token(subject=str(user.id), token_type="user")
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


@router.post("/s2s/clients", response=ServiceClientResponse, auth=jwt_auth, tags=["s2s"])
def create_service_client(request: HttpRequest, payload: CreateServiceClientRequest):
    """Create a new service client for S2S authentication"""
    try:
        credentials = auth_service.create_service_client(payload.name)
        return ServiceClientResponse(
            id=str(credentials.client.id),
            name=credentials.client.name,
            client_id=str(credentials.client.id),
            api_key_id=credentials.client.api_key_id or '',
            api_key=credentials.api_key
        )
    except ValueError as e:
        raise HttpError(409, str(e))


@router.get("/s2s/ping", auth=api_key_auth, tags=["s2s"])
def s2s_ping(request: HttpRequest):
    """S2S authentication ping endpoint"""
    client = request.auth
    return {
        "ok": True,
        "identity": {
            "method": "api_key",
            "client_id": str(client.id),
            "name": client.name
        }
    }
