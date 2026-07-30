"""
Config management services - Simplified
"""
import hashlib
import logging
import uuid
from typing import Dict, List, Optional

from cryptography.fernet import InvalidToken
from django.conf import settings
from django.core.cache import cache

from common.encryption import EncryptionService
from config_management.models import Application, ConfigEntry, Environment, FeatureFlag

logger = logging.getLogger(__name__)


class ConfigService:
    """Configuration and secret management service (unified)"""

    def __init__(self):
        self.encryption_service = EncryptionService()
        self.cache_timeout = getattr(settings, "CACHE_TIMEOUT", 300)

    def _get_or_create_scope(self, service: str, environment: str) -> tuple[Application, Environment]:
        application, _ = Application.objects.get_or_create(name=service)
        env, _ = Environment.objects.get_or_create(application=application, name=environment)
        return application, env

    def _get_scope(self, service: str, environment: str) -> tuple[Optional[Application], Optional[Environment]]:
        try:
            application = Application.objects.get(name=service)
        except Application.DoesNotExist:
            return None, None

        try:
            env = Environment.objects.get(application=application, name=environment)
        except Environment.DoesNotExist:
            return application, None

        return application, env

    @staticmethod
    def _scope_version_key(service: str, environment: str) -> str:
        return f"config:scope-version:{service}:{environment}"

    def _get_scope_version(self, service: str, environment: str) -> int:
        version = cache.get(self._scope_version_key(service, environment))
        return int(version) if version is not None else 1

    def _bump_scope_version(self, service: str, environment: str) -> None:
        version_key = self._scope_version_key(service, environment)
        current = cache.get(version_key)
        if current is None:
            cache.set(version_key, 2, None)
            return
        try:
            cache.incr(version_key)
        except ValueError:
            cache.set(version_key, int(current) + 1, None)

    def invalidate_scope_cache(self, service: str, environment: str) -> None:
        """Invalidate cached config payloads for a service/environment scope."""
        self._bump_scope_version(service, environment)

    def upsert_config(
        self,
        service: str,
        environment: str,
        key: str,
        value: str,
        is_secret: bool = False,
        config_type: str = "string",
    ) -> ConfigEntry:
        """Create or update a configuration/secret entry"""
        encrypted_value = self.encryption_service.encrypt_for_storage(value)
        application, env = self._get_or_create_scope(service, environment)

        config, created = ConfigEntry.objects.update_or_create(
            application=application,
            environment=env,
            key=key,
            defaults={
                "value": encrypted_value,
                "is_secret": is_secret,
                "type": config_type,
            },
        )
        self._bump_scope_version(service, environment)
        logger.info(
            "%s config %s/%s/%s (secret=%s)",
            "Created" if created else "Updated", service, environment, key, is_secret,
        )
        return config

    def get_config(self, service: str, environment: str, key: str) -> Optional[ConfigEntry]:
        """Get a specific configuration or secret"""
        application, env = self._get_scope(service, environment)
        if not application or not env:
            return None

        try:
            return ConfigEntry.objects.get(
                application=application,
                environment=env,
                key=key,
            )
        except ConfigEntry.DoesNotExist:
            return None

    def get_config_with_scope(self, service: str, environment: str, key: str) -> Optional[ConfigEntry]:
        """Get a specific configuration with related scope preloaded."""
        application, env = self._get_scope(service, environment)
        if not application or not env:
            return None

        try:
            return (
                ConfigEntry.objects.select_related("application", "environment")
                .get(
                    application=application,
                    environment=env,
                    key=key,
                )
            )
        except ConfigEntry.DoesNotExist:
            return None

    def list_configs(self, service: str, environment: str) -> List[ConfigEntry]:
        """List all configurations for a service/environment"""
        application, env = self._get_scope(service, environment)
        if not application or not env:
            return []

        return list(
            ConfigEntry.objects.select_related("application", "environment")
            .filter(
                application=application,
                environment=env,
            )
        )

    def decrypt_config_value(self, config: ConfigEntry) -> str:
        """Decrypt a config value for internal use"""
        return self.encryption_service.decrypt_from_storage(config.value)

    def decrypt_config_value_or_original(self, config: ConfigEntry) -> str:
        """Decrypt value when possible, fallback to original for legacy plaintext rows."""
        try:
            return self.decrypt_config_value(config)
        except InvalidToken:
            return config.value

    def delete_config(self, config_id: str) -> bool:
        """Delete a configuration entry"""
        try:
            config = ConfigEntry.objects.get(id=uuid.UUID(config_id))
            service = config.application.name
            environment = config.environment.name
            key = config.key
            config.delete()
            self._bump_scope_version(service, environment)
            logger.info("Deleted config %s/%s/%s", service, environment, key)
            return True
        except (ConfigEntry.DoesNotExist, ValueError):
            logger.warning("Config delete failed: %s not found", config_id)
            return False

    def get_config_for_client(
        self,
        service: str,
        environment: str,
        key: str,
        client_encryption_key: str,
    ) -> Optional[Dict]:
        """Get a config encrypted for a specific client"""
        config = self.get_config_with_scope(service, environment, key)
        if not config:
            return None

        try:
            decrypted_value = self.encryption_service.decrypt_from_storage(config.value)
        except InvalidToken as e:
            logger.error(
                "Failed to decrypt config %s/%s/%s: value is not valid under MASTER_ENCRYPTION_KEY",
                service, environment, key,
            )
            raise ValueError(f"Config '{key}' could not be decrypted") from e

        client_encrypted_value = self.encryption_service.re_encrypt_for_client(
            decrypted_value,
            client_encryption_key,
        )

        return {
            "id": str(config.id),
            "service": config.application.name,
            "environment": config.environment.name,
            "key": config.key,
            "value": client_encrypted_value,
            "type": config.type,
            "is_secret": config.is_secret,
        }

    def list_configs_for_client(
        self,
        service: str,
        environment: str,
        client_encryption_key: str,
    ) -> List[Dict]:
        """List all configs for a service/environment, encrypted for a specific client"""
        client_key_hash = hashlib.sha256(client_encryption_key.encode("utf-8")).hexdigest()[:16]
        scope_version = self._get_scope_version(service, environment)
        cache_key = (
            f"config:list:{service}:{environment}:{client_key_hash}:v{scope_version}"
        )
        cached_result = cache.get(cache_key)
        if cached_result is not None:
            return cached_result

        configs = self.list_configs(service, environment)

        result = []
        for config in configs:
            try:
                decrypted_value = self.encryption_service.decrypt_from_storage(config.value)
            except InvalidToken:
                # One corrupted/undecryptable row shouldn't take down the whole
                # list for every client polling this scope - skip and flag it.
                logger.error(
                    "Skipping config %s/%s/%s in client list: value is not valid under MASTER_ENCRYPTION_KEY",
                    service, environment, config.key,
                )
                continue

            client_encrypted_value = self.encryption_service.re_encrypt_for_client(
                decrypted_value,
                client_encryption_key,
            )

            result.append(
                {
                    "id": str(config.id),
                    "service": config.application.name,
                    "environment": config.environment.name,
                    "key": config.key,
                    "value": client_encrypted_value,
                    "type": config.type,
                    "is_secret": config.is_secret,
                }
            )

        cache.set(cache_key, result, self.cache_timeout)
        return result

    def list_services(self) -> List[str]:
        """List all unique applications used by configs"""
        return list(
            Application.objects.values_list("name", flat=True).distinct().order_by("name")
        )

    def list_environments(self, service: str) -> List[str]:
        """List all environments for a service/application"""
        try:
            application = Application.objects.get(name=service)
        except Application.DoesNotExist:
            return []

        return list(
            Environment.objects.filter(application=application)
            .values_list("name", flat=True)
            .distinct()
            .order_by("name")
        )


class FeatureFlagService:
    """
    Feature flag management service
    """

    def __init__(self):
        self.cache_timeout = getattr(settings, "CACHE_TIMEOUT", 300)

    def _get_scope(self, service: str, environment: str) -> tuple[Application, Environment]:
        application = Application.objects.get(name=service)
        env = Environment.objects.get(application=application, name=environment)
        return application, env

    @staticmethod
    def _scope_version_key(service: str, environment: str) -> str:
        return f"flag:scope-version:{service}:{environment}"

    def _get_scope_version(self, service: str, environment: str) -> int:
        version = cache.get(self._scope_version_key(service, environment))
        return int(version) if version is not None else 1

    def _bump_scope_version(self, service: str, environment: str) -> None:
        version_key = self._scope_version_key(service, environment)
        current = cache.get(version_key)
        if current is None:
            cache.set(version_key, 2, None)
            return
        try:
            cache.incr(version_key)
        except ValueError:
            cache.set(version_key, int(current) + 1, None)

    def invalidate_scope_cache(self, service: str, environment: str) -> None:
        """Invalidate cached feature-flag lists for a service/environment scope."""
        self._bump_scope_version(service, environment)

    def create_flag(
        self,
        service: str,
        name: str,
        description: str = "",
        is_enabled: bool = False,
        environment: Optional[str] = None,
        create_all_environments: bool = True,
    ) -> List[FeatureFlag]:
        """
        Create a new feature flag

        Args:
            name: Flag name
            description: Flag description
            is_enabled: Initial enabled state

        Returns:
            List of created/updated FeatureFlag objects

        Raises:
            ValueError: If application or environment is invalid
        """
        application = Application.objects.get(name=service)

        environments = Environment.objects.filter(application=application).order_by("name")
        if not create_all_environments:
            if not environment:
                raise ValueError("Environment is required when create_all_environments is false")
            environments = environments.filter(name=environment)

        environments = list(environments)
        if not environments:
            raise ValueError("No environments found for the selected application")

        flags: List[FeatureFlag] = []
        for env in environments:
            flag, _ = FeatureFlag.objects.update_or_create(
                application=application,
                environment=env,
                name=name,
                defaults={
                    "description": description,
                    "is_enabled": is_enabled,
                    "deleted_at": None,
                },
            )
            flags.append(flag)
            self._bump_scope_version(service, env.name)

        logger.info("Created/updated feature flag %s/%s across %d environment(s)", service, name, len(flags))
        return flags

    def get_flag(self, service: str, environment: str, name: str) -> Optional[FeatureFlag]:
        """
        Get a feature flag by name

        Args:
            name: Flag name

        Returns:
            FeatureFlag object if found, None otherwise
        """
        try:
            application, env = self._get_scope(service, environment)
            return FeatureFlag.objects.get(
                application=application,
                environment=env,
                name=name,
                deleted_at__isnull=True,
            )
        except (FeatureFlag.DoesNotExist, Application.DoesNotExist, Environment.DoesNotExist):
            return None

    def list_flags(self, service: str, environment: str) -> List[FeatureFlag]:
        """
        List all active feature flags

        Returns:
            List of FeatureFlag objects
        """
        try:
            application, env = self._get_scope(service, environment)
        except (Application.DoesNotExist, Environment.DoesNotExist):
            return []

        scope_version = self._get_scope_version(service, environment)
        cache_key = f"flag:list:{service}:{environment}:v{scope_version}"
        cached_flags = cache.get(cache_key)
        if cached_flags is not None:
            return cached_flags

        flags = list(
            FeatureFlag.objects.select_related("application", "environment")
            .filter(
                application=application,
                environment=env,
                deleted_at__isnull=True,
            )
        )
        cache.set(cache_key, flags, self.cache_timeout)
        return flags

    def toggle_flag(self, service: str, environment: str, name: str) -> Optional[FeatureFlag]:
        """
        Toggle a feature flag's enabled state

        Args:
            name: Flag name

        Returns:
            Updated FeatureFlag object if found, None otherwise
        """
        try:
            application, env = self._get_scope(service, environment)
            flag = FeatureFlag.objects.get(
                application=application,
                environment=env,
                name=name,
                deleted_at__isnull=True,
            )
            flag.is_enabled = not flag.is_enabled
            flag.save()
            self._bump_scope_version(service, environment)
            logger.info("Toggled feature flag %s/%s/%s to %s", service, environment, name, flag.is_enabled)
            return flag
        except (FeatureFlag.DoesNotExist, Application.DoesNotExist, Environment.DoesNotExist):
            logger.warning("Feature flag toggle failed: %s/%s/%s not found", service, environment, name)
            return None
