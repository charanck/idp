"""
Config Management admin configuration
"""
from django.contrib import admin
from .models import ConfigEntry, FeatureFlag, Activity


@admin.register(ConfigEntry)
class ConfigEntryAdmin(admin.ModelAdmin):
    list_display = ['service', 'environment', 'key', 'type', 'is_secret', 'created_at']
    list_filter = ['service', 'environment', 'type', 'is_secret']
    search_fields = ['service', 'environment', 'key']
    readonly_fields = ['id', 'created_at', 'updated_at']
    ordering = ['service', 'environment', 'key']


@admin.register(FeatureFlag)
class FeatureFlagAdmin(admin.ModelAdmin):
    list_display = ['name', 'is_enabled', 'created_at', 'deleted_at']
    list_filter = ['is_enabled']
    search_fields = ['name', 'description']
    readonly_fields = ['id', 'created_at', 'updated_at']
    ordering = ['name']


@admin.register(Activity)
class ActivityAdmin(admin.ModelAdmin):
    list_display = ['timestamp', 'type', 'resource', 'resource_name', 'user_email']
    list_filter = ['type', 'resource', 'timestamp']
    search_fields = ['resource_name', 'user_email', 'resource_id']
    readonly_fields = ['id', 'type', 'resource', 'resource_id', 'resource_name', 'user_email', 'details', 'ip_address', 'timestamp']
    ordering = ['-timestamp']
    
    def has_add_permission(self, request):
        """Activity logs cannot be manually added"""
        return False
    
    def has_delete_permission(self, request, obj=None):
        """Activity logs cannot be deleted"""
        return False
    
    def has_change_permission(self, request, obj=None):
        """Activity logs cannot be changed"""
        return False

