"""
Integration tests for config_management/services.py - uses the sqlite test
database (see control_plane_project/test_settings.py).
"""
import uuid

import pytest
from django.utils import timezone

from common.encryption import generate_encryption_key
from config_management.models import Application, ConfigEntry, Environment, FeatureFlag
from config_management.services import ConfigService, FeatureFlagService

pytestmark = pytest.mark.django_db

config_service = ConfigService()
flag_service = FeatureFlagService()


class TestUpsertConfig:
    def test_creates_application_and_environment_on_first_write(self):
        config_service.upsert_config("payments", "prod", "API_URL", "https://api.example.com")

        assert Application.objects.filter(name="payments").exists()
        assert Environment.objects.filter(application__name="payments", name="prod").exists()

    def test_stores_value_encrypted_not_in_plaintext(self):
        entry = config_service.upsert_config("payments", "prod", "API_URL", "https://api.example.com")

        assert entry.value != "https://api.example.com"
        assert config_service.decrypt_config_value(entry) == "https://api.example.com"

    def test_updates_existing_entry_instead_of_duplicating(self):
        config_service.upsert_config("payments", "prod", "API_URL", "https://v1.example.com")
        updated = config_service.upsert_config("payments", "prod", "API_URL", "https://v2.example.com")

        assert ConfigEntry.objects.filter(application__name="payments", key="API_URL").count() == 1
        assert config_service.decrypt_config_value(updated) == "https://v2.example.com"

    def test_reuses_scope_across_multiple_keys(self):
        config_service.upsert_config("payments", "prod", "KEY_A", "a")
        config_service.upsert_config("payments", "prod", "KEY_B", "b")

        assert Application.objects.filter(name="payments").count() == 1
        assert Environment.objects.filter(application__name="payments", name="prod").count() == 1


class TestGetAndListConfigs:
    def test_get_config_returns_none_for_unknown_scope(self):
        assert config_service.get_config("unknown-app", "prod", "KEY") is None

    def test_get_config_returns_none_for_unknown_key_in_known_scope(self):
        config_service.upsert_config("payments", "prod", "KEY_A", "a")

        assert config_service.get_config("payments", "prod", "KEY_B") is None

    def test_get_config_finds_existing_entry(self):
        config_service.upsert_config("payments", "prod", "KEY_A", "a")

        found = config_service.get_config("payments", "prod", "KEY_A")

        assert found is not None
        assert found.key == "KEY_A"

    def test_list_configs_scopes_to_service_and_environment(self):
        config_service.upsert_config("payments", "prod", "KEY_A", "a")
        config_service.upsert_config("payments", "staging", "KEY_A", "a-staging")
        config_service.upsert_config("other-app", "prod", "KEY_A", "other")

        results = config_service.list_configs("payments", "prod")

        assert [c.key for c in results] == ["KEY_A"]

    def test_list_configs_returns_empty_for_unknown_scope(self):
        assert config_service.list_configs("unknown", "prod") == []


class TestDecryptConfigValue:
    def test_decrypt_config_value_or_original_returns_plaintext_for_legacy_row(self):
        application = Application.objects.create(name="legacy-app")
        env = Environment.objects.create(application=application, name="prod")
        legacy_entry = ConfigEntry.objects.create(
            application=application, environment=env, key="LEGACY", value="plaintext-value"
        )

        assert config_service.decrypt_config_value_or_original(legacy_entry) == "plaintext-value"

    def test_decrypt_config_value_or_original_decrypts_normal_row(self):
        entry = config_service.upsert_config("payments", "prod", "KEY_A", "secret-value")

        assert config_service.decrypt_config_value_or_original(entry) == "secret-value"


class TestDeleteConfig:
    def test_deletes_existing_config_and_bumps_scope_version(self):
        entry = config_service.upsert_config("payments", "prod", "KEY_A", "a")

        assert config_service.delete_config(str(entry.id)) is True
        assert not ConfigEntry.objects.filter(id=entry.id).exists()

    def test_returns_false_for_unknown_id(self):
        assert config_service.delete_config(str(uuid.uuid4())) is False

    def test_returns_false_for_malformed_id(self):
        assert config_service.delete_config("not-a-uuid") is False


class TestClientFacingConfigAccess:
    def test_get_config_for_client_reencrypts_with_client_key(self):
        config_service.upsert_config("payments", "prod", "API_URL", "https://api.example.com")
        client_key = generate_encryption_key()

        result = config_service.get_config_for_client("payments", "prod", "API_URL", client_key)

        assert result is not None
        assert result["value"] != "https://api.example.com"
        from common.encryption import decrypt_value
        assert decrypt_value(result["value"], client_key) == "https://api.example.com"

    def test_get_config_for_client_returns_none_when_config_missing(self):
        client_key = generate_encryption_key()
        assert config_service.get_config_for_client("payments", "prod", "MISSING", client_key) is None

    def test_list_configs_for_client_encrypts_each_entry_for_the_client(self):
        config_service.upsert_config("payments", "prod", "KEY_A", "a")
        config_service.upsert_config("payments", "prod", "KEY_B", "b")
        client_key = generate_encryption_key()

        results = config_service.list_configs_for_client("payments", "prod", client_key)

        assert {r["key"] for r in results} == {"KEY_A", "KEY_B"}

    def test_list_configs_for_client_skips_entries_not_decryptable_under_master_key(self):
        application = Application.objects.create(name="payments")
        env = Environment.objects.create(application=application, name="prod")
        ConfigEntry.objects.create(application=application, environment=env, key="GOOD", value="not-valid-fernet-data")
        client_key = generate_encryption_key()

        results = config_service.list_configs_for_client("payments", "prod", client_key)

        assert results == []

    def test_list_configs_for_client_is_cached_until_scope_invalidated(self, settings):
        settings.CACHE_TIMEOUT = 300
        config_service.upsert_config("payments", "prod", "KEY_A", "a")
        client_key = generate_encryption_key()

        first = config_service.list_configs_for_client("payments", "prod", client_key)
        # Mutate the row directly, bypassing upsert_config's cache invalidation.
        ConfigEntry.objects.filter(key="KEY_A").update(value="corrupted-and-should-not-be-seen")

        second = config_service.list_configs_for_client("payments", "prod", client_key)

        assert first == second

    def test_list_configs_for_client_cache_invalidated_after_upsert(self):
        config_service.upsert_config("payments", "prod", "KEY_A", "a")
        client_key = generate_encryption_key()
        config_service.list_configs_for_client("payments", "prod", client_key)

        config_service.upsert_config("payments", "prod", "KEY_A", "a-updated")
        results = config_service.list_configs_for_client("payments", "prod", client_key)

        from common.encryption import decrypt_value
        assert decrypt_value(results[0]["value"], client_key) == "a-updated"


class TestListServicesAndEnvironments:
    def test_list_services_returns_distinct_sorted_names(self):
        # A schema migration seeds a baseline "global" application, so assert
        # our new services are present and correctly sorted relative to each
        # other rather than asserting on the full list contents.
        config_service.upsert_config("zeta-app", "prod", "K", "v")
        config_service.upsert_config("alpha-app", "prod", "K", "v")
        config_service.upsert_config("zeta-app", "prod", "K2", "v")  # should not duplicate zeta-app

        services = config_service.list_services()

        assert services == sorted(services)
        assert services.index("alpha-app") < services.index("zeta-app")
        assert services.count("zeta-app") == 1

    def test_list_environments_scoped_to_application(self):
        config_service.upsert_config("payments", "prod", "K", "v")
        config_service.upsert_config("payments", "staging", "K", "v")
        config_service.upsert_config("other-app", "dev", "K", "v")

        assert config_service.list_environments("payments") == ["prod", "staging"]

    def test_list_environments_returns_empty_for_unknown_application(self):
        assert config_service.list_environments("unknown") == []


class TestFeatureFlagServiceCreateFlag:
    def setup_method(self):
        self.application = Application.objects.create(name="payments")
        Environment.objects.create(application=self.application, name="prod")
        Environment.objects.create(application=self.application, name="staging")

    def test_creates_flag_across_all_environments_by_default(self):
        flags = flag_service.create_flag("payments", "new-checkout", description="desc", is_enabled=True)

        assert len(flags) == 2
        assert {f.environment.name for f in flags} == {"prod", "staging"}
        assert all(f.is_enabled for f in flags)

    def test_creates_flag_for_single_environment_when_requested(self):
        flags = flag_service.create_flag(
            "payments", "new-checkout", environment="prod", create_all_environments=False
        )

        assert len(flags) == 1
        assert flags[0].environment.name == "prod"

    def test_raises_when_single_environment_requested_without_name(self):
        with pytest.raises(ValueError, match="Environment is required"):
            flag_service.create_flag("payments", "new-checkout", create_all_environments=False)

    def test_raises_when_no_environments_exist_for_application(self):
        Application.objects.create(name="empty-app")

        with pytest.raises(ValueError, match="No environments found"):
            flag_service.create_flag("empty-app", "new-flag")

    def test_raises_when_application_unknown(self):
        with pytest.raises(ValueError, match="does not exist"):
            flag_service.create_flag("unknown-app", "new-flag")

    def test_recreating_flag_undoes_previous_soft_delete(self):
        flag_service.create_flag("payments", "new-checkout")
        flag = FeatureFlag.objects.get(application=self.application, environment__name="prod", name="new-checkout")
        flag.deleted_at = timezone.now()
        flag.save()

        flag_service.create_flag("payments", "new-checkout")

        flag.refresh_from_db()
        assert flag.deleted_at is None


class TestFeatureFlagServiceReadAndToggle:
    def setup_method(self):
        self.application = Application.objects.create(name="payments")
        self.env = Environment.objects.create(application=self.application, name="prod")

    def test_get_flag_returns_none_when_missing(self):
        assert flag_service.get_flag("payments", "prod", "missing-flag") is None

    def test_get_flag_returns_none_when_soft_deleted(self):
        flag_service.create_flag("payments", "my-flag", create_all_environments=False, environment="prod")
        flag = FeatureFlag.objects.get(name="my-flag")
        flag.deleted_at = timezone.now()
        flag.save()

        assert flag_service.get_flag("payments", "prod", "my-flag") is None

    def test_list_flags_excludes_soft_deleted(self):
        flag_service.create_flag("payments", "keep-me", create_all_environments=False, environment="prod")
        flag_service.create_flag("payments", "delete-me", create_all_environments=False, environment="prod")
        FeatureFlag.objects.filter(name="delete-me").update(deleted_at=timezone.now())

        flags = flag_service.list_flags("payments", "prod")

        assert [f.name for f in flags] == ["keep-me"]

    def test_list_flags_returns_empty_for_unknown_scope(self):
        assert flag_service.list_flags("unknown", "prod") == []

    def test_toggle_flag_flips_state(self):
        flag_service.create_flag("payments", "my-flag", create_all_environments=False, environment="prod", is_enabled=False)

        toggled = flag_service.toggle_flag("payments", "prod", "my-flag")
        assert toggled.is_enabled is True

        toggled_again = flag_service.toggle_flag("payments", "prod", "my-flag")
        assert toggled_again.is_enabled is False

    def test_toggle_flag_returns_none_for_missing_flag(self):
        assert flag_service.toggle_flag("payments", "prod", "missing-flag") is None

    def test_toggle_flag_invalidates_list_cache(self):
        flag_service.create_flag("payments", "my-flag", create_all_environments=False, environment="prod", is_enabled=False)
        cached_before = flag_service.list_flags("payments", "prod")
        assert cached_before[0].is_enabled is False

        flag_service.toggle_flag("payments", "prod", "my-flag")

        cached_after = flag_service.list_flags("payments", "prod")
        assert cached_after[0].is_enabled is True
