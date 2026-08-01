"""
Authentication app models
"""
import uuid
from django.contrib.auth.models import AbstractUser
from django.db import models


class User(AbstractUser):
    """
    Custom User model extending Django's AbstractUser
    """
    id = models.UUIDField(primary_key=True, default=uuid.uuid4, editable=False)
    email = models.EmailField(unique=True, db_index=True)
    is_active = models.BooleanField(
        default=False,
        help_text="Unset until an admin activates this user. Inactive users cannot log in.",
    )
    force_password_reset = models.BooleanField(
        default=False,
        help_text="Force user to reset password on next login"
    )
    created_at = models.DateTimeField(auto_now_add=True)
    updated_at = models.DateTimeField(auto_now=True)
    
    USERNAME_FIELD = 'email'
    REQUIRED_FIELDS = ['username']
    
    class Meta:
        db_table = 'users'
        verbose_name = 'User'
        verbose_name_plural = 'Users'
        indexes = [
            models.Index(fields=['-created_at']),
            models.Index(fields=['is_active', '-created_at']),
        ]
    
    def __str__(self):
        return self.email


class ServiceClient(models.Model):
    """
    Service-to-Service authentication client
    """
    id = models.UUIDField(primary_key=True, default=uuid.uuid4, editable=False)
    name = models.CharField(max_length=120, unique=True, db_index=True)
    api_key_id = models.CharField(
        max_length=80,
        unique=True,
        db_index=True,
        blank=True,
        null=True,
        help_text="Public identifier for the service client API key"
    )
    api_key_hash = models.CharField(
        max_length=255,
        blank=True,
        default='',
        help_text="Hash of the secret part of the API key"
    )
    encryption_key = models.CharField(
        max_length=255,
        default='',  # Temporary default for migration
        help_text="Fernet encryption key for encrypting configs/secrets for this client"
    )
    is_active = models.BooleanField(default=True)
    created_at = models.DateTimeField(auto_now_add=True)
    updated_at = models.DateTimeField(auto_now=True)
    
    class Meta:
        db_table = 'service_clients'
        verbose_name = 'Service Client'
        verbose_name_plural = 'Service Clients'
        indexes = [
            models.Index(fields=['-created_at']),
            models.Index(fields=['is_active', '-created_at']),
        ]
        
    def __str__(self):
        return self.name


# Import OAuth models here to include them in migrations
from .oauth_models import OAuthProvider, OAuthUserToken
