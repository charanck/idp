"""
OAuth2/OIDC Provider models
"""
import uuid
from django.db import models
from django.core.validators import URLValidator


class OAuthProvider(models.Model):
    """
    OAuth2/OIDC Provider Configuration
    Supports authorization code flow for user authentication
    """
    id = models.UUIDField(primary_key=True, default=uuid.uuid4, editable=False)
    name = models.CharField(
        max_length=120, 
        unique=True, 
        db_index=True,
        help_text="Provider name (e.g., 'Google', 'GitHub', 'Okta')"
    )
    
    # OAuth2 Configuration
    client_id = models.CharField(max_length=255, help_text="OAuth2 Client ID")
    client_secret = models.CharField(max_length=255, help_text="OAuth2 Client Secret")
    
    # Endpoints
    authorization_url = models.URLField(
        max_length=500,
        help_text="Authorization endpoint URL (for user login)"
    )
    token_url = models.URLField(
        max_length=500,
        help_text="Token endpoint URL"
    )
    userinfo_url = models.URLField(
        max_length=500,
        blank=True,
        null=True,
        help_text="Userinfo endpoint URL (OIDC)"
    )
    
    # Configuration
    scope = models.CharField(
        max_length=255,
        default='openid email profile',
        help_text="OAuth2 scopes (space-separated)"
    )
    
    # Settings
    is_active = models.BooleanField(default=True)
    auto_create_users = models.BooleanField(
        default=True,
        help_text="Automatically create users on first OAuth login"
    )
    
    # Metadata
    created_at = models.DateTimeField(auto_now_add=True)
    updated_at = models.DateTimeField(auto_now=True)
    
    class Meta:
        db_table = 'oauth_providers'
        verbose_name = 'OAuth Provider'
        verbose_name_plural = 'OAuth Providers'
        indexes = [
            models.Index(fields=['is_active', '-created_at']),
        ]
    
    def __str__(self):
        return self.name


class OAuthUserToken(models.Model):
    """
    Store OAuth tokens for users
    """
    id = models.UUIDField(primary_key=True, default=uuid.uuid4, editable=False)
    user = models.ForeignKey(
        'authentication.User',
        on_delete=models.CASCADE,
        related_name='oauth_tokens'
    )
    provider = models.ForeignKey(
        OAuthProvider,
        on_delete=models.CASCADE,
        related_name='user_tokens'
    )
    
    # OAuth tokens
    access_token = models.TextField(help_text="OAuth access token")
    refresh_token = models.TextField(blank=True, null=True, help_text="OAuth refresh token")
    token_type = models.CharField(max_length=50, default='Bearer')
    expires_at = models.DateTimeField(blank=True, null=True)
    
    # Provider user info
    provider_user_id = models.CharField(
        max_length=255,
        help_text="User ID from OAuth provider"
    )
    provider_user_email = models.EmailField(blank=True, null=True)
    
    created_at = models.DateTimeField(auto_now_add=True)
    updated_at = models.DateTimeField(auto_now=True)
    
    class Meta:
        db_table = 'oauth_user_tokens'
        verbose_name = 'OAuth User Token'
        verbose_name_plural = 'OAuth User Tokens'
        unique_together = [['user', 'provider']]
        indexes = [
            models.Index(fields=['provider', 'provider_user_id']),
        ]
    
    def __str__(self):
        return f"{self.user.email} - {self.provider.name}"
