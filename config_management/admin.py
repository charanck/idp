"""
Config Management admin configuration
"""
from django.contrib import admin
from .models import Application, Environment, ConfigEntry, ConfigEntryVersion, FeatureFlag, Activity


@admin.register(Application)
class ApplicationAdmin(admin.ModelAdmin):
    list_display = ["name", "created_at"]
    search_fields = ["name"]
    readonly_fields = ["id", "created_at", "updated_at"]
    ordering = ["name"]


@admin.register(Environment)
class EnvironmentAdmin(admin.ModelAdmin):
    list_display = ["name", "application", "created_at"]
    list_filter = ["application"]
    search_fields = ["name", "application__name"]
    readonly_fields = ["id", "created_at", "updated_at"]
    ordering = ["application__name", "name"]


@admin.register(ConfigEntry)
class ConfigEntryAdmin(admin.ModelAdmin):
    list_display = ["application", "environment", "key", "type", "is_secret", "created_at"]
    list_filter = ["application", "environment", "type", "is_secret"]
    search_fields = ["application__name", "environment__name", "key"]
    readonly_fields = ["id", "created_at", "updated_at"]
    ordering = ["application__name", "environment__name", "key"]


@admin.register(ConfigEntryVersion)
class ConfigEntryVersionAdmin(admin.ModelAdmin):
    list_display = ["config_entry", "version", "action", "is_secret", "changed_by", "created_at"]
    list_filter = ["action", "is_secret"]
    search_fields = ["config_entry__key", "changed_by"]
    readonly_fields = [f.name for f in ConfigEntryVersion._meta.fields]
    ordering = ["config_entry", "-version"]

    def has_add_permission(self, request):
        return False

    def has_delete_permission(self, request, obj=None):
        return False

    def has_change_permission(self, request, obj=None):
        return False


@admin.register(FeatureFlag)
class FeatureFlagAdmin(admin.ModelAdmin):
    list_display = ["name", "application", "environment", "is_enabled", "created_at", "deleted_at"]
    list_filter = ["application", "environment", "is_enabled"]
    search_fields = ["name", "description", "application__name", "environment__name"]
    readonly_fields = ["id", "created_at", "updated_at"]
    ordering = ["application__name", "environment__name", "name"]


@admin.register(Activity)
class ActivityAdmin(admin.ModelAdmin):
    list_display = ["timestamp", "type", "resource", "resource_name", "user_email"]
    list_filter = ["type", "resource", "timestamp"]
    search_fields = ["resource_name", "user_email", "resource_id"]
    readonly_fields = [
        "id",
        "type",
        "resource",
        "resource_id",
        "resource_name",
        "user_email",
        "details",
        "ip_address",
        "timestamp",
    ]
    ordering = ["-timestamp"]

    def has_add_permission(self, request):
        return False

    def has_delete_permission(self, request, obj=None):
        return False

    def has_change_permission(self, request, obj=None):
        return False
