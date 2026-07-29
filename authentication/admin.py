"""
Authentication admin configuration
"""
from django.contrib import admin
from django.contrib.auth.admin import UserAdmin as BaseUserAdmin
from .models import User, ServiceClient
from .oauth_models import OAuthProvider, OAuthUserToken


@admin.register(User)
class UserAdmin(BaseUserAdmin):
    list_display = ['email', 'username', 'is_active', 'is_staff', 'created_at']
    list_filter = ['is_active', 'is_staff', 'is_superuser']
    search_fields = ['email', 'username']
    ordering = ['-created_at']
    
    fieldsets = (
        (None, {'fields': ('email', 'username', 'password')}),
        ('Permissions', {'fields': ('is_active', 'is_staff', 'is_superuser', 'groups', 'user_permissions')}),
        ('Important dates', {'fields': ('last_login', 'date_joined', 'created_at', 'updated_at')}),
    )
    readonly_fields = ['created_at', 'updated_at', 'date_joined', 'last_login']


@admin.register(ServiceClient)
class ServiceClientAdmin(admin.ModelAdmin):
    list_display = ['name', 'api_key_id', 'is_active', 'created_at']
    list_filter = ['is_active']
    search_fields = ['name', 'api_key_id']
    readonly_fields = ['id', 'created_at', 'updated_at', 'api_key_id', 'api_key_hash']
    ordering = ['-created_at']


@admin.register(OAuthProvider)
class OAuthProviderAdmin(admin.ModelAdmin):
    list_display = ['name', 'is_active', 'auto_create_users', 'created_at']
    list_filter = ['is_active', 'auto_create_users']
    search_fields = ['name']
    readonly_fields = ['id', 'created_at', 'updated_at']
    ordering = ['-created_at']
    
    fieldsets = (
        ('Basic Information', {
            'fields': ('name', 'is_active', 'auto_create_users')
        }),
        ('OAuth2 Credentials', {
            'fields': ('client_id', 'client_secret')
        }),
        ('Endpoints', {
            'fields': ('authorization_url', 'token_url', 'userinfo_url')
        }),
        ('Configuration', {
            'fields': ('scope',)
        }),
        ('Metadata', {
            'fields': ('id', 'created_at', 'updated_at'),
            'classes': ('collapse',)
        }),
    )


@admin.register(OAuthUserToken)
class OAuthUserTokenAdmin(admin.ModelAdmin):
    list_display = ['user', 'provider', 'provider_user_email', 'expires_at', 'created_at']
    list_filter = ['provider']
    search_fields = ['user__email', 'provider_user_email', 'provider_user_id']
    readonly_fields = ['id', 'created_at', 'updated_at']
    ordering = ['-created_at']

