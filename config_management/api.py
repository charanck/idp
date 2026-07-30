"""
Config Management API endpoints
"""
import logging

from ninja import Router
from ninja.errors import HttpError
from django.http import HttpRequest
from typing import List

from config_management.schemas import (
    ConfigUpsertRequest,
    ConfigResponse,
    ConfigRollbackRequest,
    ConfigVersionResponse,
    FeatureFlagCreateRequest,
    FeatureFlagResponse,
)
from config_management.services import ConfigService, FeatureFlagService
from common.activity_logger import log_create, log_toggle, log_update
from common.authentication import JWTAuth, APIKeyAuth

logger = logging.getLogger(__name__)

router = Router()
config_service = ConfigService()
flag_service = FeatureFlagService()
jwt_auth = JWTAuth()
apikey_auth = APIKeyAuth()


@router.post("/configs/upsert", response=ConfigResponse, auth=jwt_auth, tags=["config"])
def upsert_config(request: HttpRequest, payload: ConfigUpsertRequest):
    """Create or update a configuration entry"""
    existed_before = config_service.get_config(payload.service, payload.environment, payload.key) is not None

    try:
        entry = config_service.upsert_config(
            service=payload.service,
            environment=payload.environment,
            key=payload.key,
            value=payload.value,
            is_secret=payload.is_secret,
            config_type=payload.type
        )
    except Exception:
        logger.exception(
            "Unexpected error upserting config %s/%s/%s", payload.service, payload.environment, payload.key
        )
        raise HttpError(500, "Failed to save configuration")

    config_name = f"{entry.application.name}/{entry.environment.name}/{entry.key}"
    details = {'is_secret': entry.is_secret, 'type': entry.type}
    if existed_before:
        log_update('config', str(entry.id), config_name, request=request, details=details)
    else:
        log_create('config', str(entry.id), config_name, request=request, details=details)

    # Don't return the actual value over the admin API - not even encrypted.
    return ConfigResponse(
        id=str(entry.id),
        service=entry.application.name,
        environment=entry.environment.name,
        key=entry.key,
        value="***ENCRYPTED***",
        is_secret=entry.is_secret,
        type=entry.type
    )


@router.get("/configs/{config_id}/history", response=List[ConfigVersionResponse], auth=jwt_auth, tags=["config"])
def get_config_history(request: HttpRequest, config_id: str):
    """List version history for a config entry, newest first."""
    versions = config_service.get_config_history(config_id)

    return [
        ConfigVersionResponse(
            version=v.version,
            action=v.action,
            value=None if v.is_secret else config_service.decrypt_version_value(v),
            is_secret=v.is_secret,
            type=v.type,
            changed_by=v.changed_by,
            created_at=v.created_at.isoformat(),
        )
        for v in versions
    ]


@router.post("/configs/{config_id}/rollback", response=ConfigResponse, auth=jwt_auth, tags=["config"])
def rollback_config(request: HttpRequest, config_id: str, payload: ConfigRollbackRequest):
    """Roll back a config entry to a prior version, recorded as a new version."""
    try:
        entry = config_service.rollback_config(
            config_id,
            payload.version,
            changed_by=getattr(request.auth, "email", None),
        )
    except ValueError as e:
        raise HttpError(409, str(e))
    except Exception:
        logger.exception("Unexpected error rolling back config %s to version %s", config_id, payload.version)
        raise HttpError(500, "Failed to roll back configuration")

    if entry is None:
        raise HttpError(404, "Config or version not found")

    config_name = f"{entry.application.name}/{entry.environment.name}/{entry.key}"
    log_update(
        'config', str(entry.id), config_name, request=request,
        details={'is_secret': entry.is_secret, 'rolled_back_to_version': payload.version},
    )

    return ConfigResponse(
        id=str(entry.id),
        service=entry.application.name,
        environment=entry.environment.name,
        key=entry.key,
        value="***ENCRYPTED***",
        is_secret=entry.is_secret,
        type=entry.type,
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

    try:
        configs = config_service.list_configs_for_client(
            service=service,
            environment=environment,
            client_encryption_key=client.encryption_key
        )
    except Exception:
        logger.exception(
            "Unexpected error listing configs for client %s (%s/%s)", client.name, service, environment
        )
        raise HttpError(500, "Failed to list configurations")

    logger.debug("Client %s fetched %d config(s) for %s/%s", client.name, len(configs), service, environment)

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
    except ValueError as e:
        raise HttpError(409, str(e))
    except Exception:
        logger.exception("Unexpected error creating feature flag %s/%s", payload.service, payload.name)
        raise HttpError(500, "Failed to create feature flag")

    flag = flags[0]
    log_create(
        'flag',
        str(flag.id),
        f"{flag.application.name}/{flag.name}",
        request=request,
        details={'is_enabled': flag.is_enabled, 'environments_affected': len(flags)},
    )
    return FeatureFlagResponse(
        id=str(flag.id),
        service=flag.application.name,
        environment=flag.environment.name,
        name=flag.name,
        description=flag.description or "",
        is_enabled=flag.is_enabled,
        created_count=len(flags),
    )


@router.get("/feature-flags", response=List[FeatureFlagResponse], auth=jwt_auth, tags=["feature-flags"])
def list_feature_flags(request: HttpRequest, service: str, environment: str):
    """List feature flags for a specific service/environment"""
    try:
        flags = flag_service.list_flags(service=service, environment=environment)
    except Exception:
        logger.exception("Unexpected error listing feature flags for %s/%s", service, environment)
        raise HttpError(500, "Failed to list feature flags")

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
    try:
        flag = flag_service.toggle_flag(service=service, environment=environment, name=name)
    except Exception:
        logger.exception("Unexpected error toggling feature flag %s/%s/%s", service, environment, name)
        raise HttpError(500, "Failed to toggle feature flag")

    if flag is None:
        raise HttpError(404, "Feature flag not found")

    log_toggle(
        'flag', str(flag.id), f"{flag.application.name}/{flag.environment.name}/{flag.name}",
        request=request, details={'is_enabled': flag.is_enabled},
    )
    return FeatureFlagResponse(
        id=str(flag.id),
        service=flag.application.name,
        environment=flag.environment.name,
        name=flag.name,
        description=flag.description or "",
        is_enabled=flag.is_enabled
    )
