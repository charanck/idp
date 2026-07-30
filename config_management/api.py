"""
Config Management API endpoints
"""
from ninja import Router
from ninja.errors import HttpError
from django.http import HttpRequest
from typing import List

from config_management.schemas import (
    ConfigUpsertRequest,
    ConfigResponse,
    FeatureFlagCreateRequest,
    FeatureFlagResponse,
)
from config_management.services import ConfigService, FeatureFlagService
from common.authentication import JWTAuth, APIKeyAuth

router = Router()
config_service = ConfigService()
flag_service = FeatureFlagService()
jwt_auth = JWTAuth()
apikey_auth = APIKeyAuth()


@router.post("/configs/upsert", response=ConfigResponse, auth=jwt_auth, tags=["config"])
def upsert_config(request: HttpRequest, payload: ConfigUpsertRequest):
    """Create or update a configuration entry"""
    entry = config_service.upsert_config(
        service=payload.service,
        environment=payload.environment,
        key=payload.key,
        value=payload.value,
        is_secret=payload.is_secret,
        config_type=payload.type
    )
    
    # For JWT auth (admin), show masked value for secrets
    shown_value = "***ENCRYPTED***" if entry.is_secret else "***ENCRYPTED***"
    return ConfigResponse(
        id=str(entry.id),
        service=entry.application.name,
        environment=entry.environment.name,
        key=entry.key,
        value=shown_value,  # Don't return actual value for security
        is_secret=entry.is_secret,
        type=entry.type
    )


@router.get("/configs/list", response=List[ConfigResponse], auth=apikey_auth, tags=["config"])
def list_configs_for_client(request: HttpRequest, service: str, environment: str):
    """
    List all configurations for a service and environment
    Values are encrypted with the client's encryption key
    Client must decrypt using their encryption key
    """
    # Get client from request (set by APIKeyAuth)
    client = request.auth
    
    # Get configs encrypted for this client
    configs = config_service.list_configs_for_client(
        service=service,
        environment=environment,
        client_encryption_key=client.encryption_key
    )
    
    return [
        ConfigResponse(
            id=config['id'],
            service=config['service'],
            environment=config['environment'],
            key=config['key'],
            value=config['value'],  # Encrypted for client
            is_secret=config['is_secret'],
            type=config['type']
        )
        for config in configs
    ]


@router.post("/feature-flags", response=FeatureFlagResponse, auth=jwt_auth, tags=["feature-flags"])
def create_feature_flag(request: HttpRequest, payload: FeatureFlagCreateRequest):
    """Create a new feature flag"""
    try:
        flags = flag_service.create_flag(
            service=payload.service,
            name=payload.name,
            description=payload.description,
            is_enabled=payload.is_enabled,
            environment=payload.environment or None,
            create_all_environments=True,
        )
        flag = flags[0]
        return FeatureFlagResponse(
            id=str(flag.id),
            service=flag.application.name,
            environment=flag.environment.name,
            name=flag.name,
            description=flag.description or "",
            is_enabled=flag.is_enabled,
            created_count=len(flags),
        )
    except ValueError as e:
        raise HttpError(409, str(e))


@router.get("/feature-flags", response=List[FeatureFlagResponse], auth=jwt_auth, tags=["feature-flags"])
def list_feature_flags(request: HttpRequest, service: str, environment: str):
    """List feature flags for a specific service/environment"""
    flags = flag_service.list_flags(service=service, environment=environment)
    return [
        FeatureFlagResponse(
            id=str(flag.id),
            service=flag.application.name,
            environment=flag.environment.name,
            name=flag.name,
            description=flag.description or "",
            is_enabled=flag.is_enabled
        )
        for flag in flags
    ]


@router.post("/feature-flags/{name}/toggle", response=FeatureFlagResponse, auth=jwt_auth, tags=["feature-flags"])
def toggle_feature_flag(request: HttpRequest, name: str, service: str, environment: str):
    """Toggle a feature flag's enabled state"""
    flag = flag_service.toggle_flag(service=service, environment=environment, name=name)
    if flag is None:
        raise HttpError(404, "Feature flag not found")
    return FeatureFlagResponse(
        id=str(flag.id),
        service=flag.application.name,
        environment=flag.environment.name,
        name=flag.name,
        description=flag.description or "",
        is_enabled=flag.is_enabled
    )
