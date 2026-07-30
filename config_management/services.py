"""
Config management services - Simplified
"""
import uuid
from typing import Dict, List, Optional

from common.encryption import EncryptionService
from config_management.models import Application, ConfigEntry, Environment, FeatureFlag


class ConfigService:
    """Configuration and secret management service (unified)"""

    def __init__(self):
        self.encryption_service = EncryptionService()

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

        config, _ = ConfigEntry.objects.update_or_create(
            application=application,
            environment=env,
            key=key,
            defaults={
                "value": encrypted_value,
                "is_secret": is_secret,
                "type": config_type,
            },
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

    def list_configs(self, service: str, environment: str) -> List[ConfigEntry]:
        """List all configurations for a service/environment"""
        application, env = self._get_scope(service, environment)
        if not application or not env:
            return []

        return list(
            ConfigEntry.objects.filter(
                application=application,
                environment=env,
            )
        )

    def decrypt_config_value(self, config: ConfigEntry) -> str:
        """Decrypt a config value for internal use"""
        return self.encryption_service.decrypt_from_storage(config.value)

    def delete_config(self, config_id: str) -> bool:
        """Delete a configuration entry"""
        try:
            config = ConfigEntry.objects.get(id=uuid.UUID(config_id))
            config.delete()
            return True
        except (ConfigEntry.DoesNotExist, ValueError):
            return False

    def get_config_for_client(
        self,
        service: str,
        environment: str,
        key: str,
        client_encryption_key: str,
    ) -> Optional[Dict]:
        """Get a config encrypted for a specific client"""
        config = self.get_config(service, environment, key)
        if not config:
            return None

        decrypted_value = self.encryption_service.decrypt_from_storage(config.value)
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
        configs = self.list_configs(service, environment)

        result = []
        for config in configs:
            decrypted_value = self.encryption_service.decrypt_from_storage(config.value)
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

    def create_flag(self, name: str, description: str = "", is_enabled: bool = False) -> FeatureFlag:
        """
        Create a new feature flag

        Args:
            name: Flag name
            description: Flag description
            is_enabled: Initial enabled state

        Returns:
            FeatureFlag object

        Raises:
            ValueError: If flag already exists
        """
        if FeatureFlag.objects.filter(name=name, deleted_at__isnull=True).exists():
            raise ValueError("Feature flag already exists")

        return FeatureFlag.objects.create(
            id=uuid.uuid4(),
            name=name,
            description=description,
            is_enabled=is_enabled,
        )

    def get_flag(self, name: str) -> Optional[FeatureFlag]:
        """
        Get a feature flag by name

        Args:
            name: Flag name

        Returns:
            FeatureFlag object if found, None otherwise
        """
        try:
            return FeatureFlag.objects.get(name=name, deleted_at__isnull=True)
        except FeatureFlag.DoesNotExist:
            return None

    def list_flags(self) -> List[FeatureFlag]:
        """
        List all active feature flags

        Returns:
            List of FeatureFlag objects
        """
        return list(FeatureFlag.objects.filter(deleted_at__isnull=True))

    def toggle_flag(self, name: str) -> Optional[FeatureFlag]:
        """
        Toggle a feature flag's enabled state

        Args:
            name: Flag name

        Returns:
            Updated FeatureFlag object if found, None otherwise
        """
        try:
            flag = FeatureFlag.objects.get(name=name, deleted_at__isnull=True)
            flag.is_enabled = not flag.is_enabled
            flag.save()
            return flag
        except FeatureFlag.DoesNotExist:
            return None
