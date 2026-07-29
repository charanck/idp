"""
Configuration Management models
"""
import uuid
from django.db import models


class ConfigEntry(models.Model):
    """
    Configuration entry for a service and environment
    """
    TYPE_CHOICES = [
        ('boolean', 'Boolean'),
        ('string', 'String'),
        ('number', 'Number'),
        ('object', 'Object'),
        ('array', 'Array'),
    ]
    
    id = models.UUIDField(primary_key=True, default=uuid.uuid4, editable=False)
    service = models.CharField(max_length=120, db_index=True)
    environment = models.CharField(max_length=120, db_index=True)
    key = models.CharField(max_length=120)
    value = models.TextField()
    type = models.CharField(max_length=20, choices=TYPE_CHOICES, default='string')
    is_secret = models.BooleanField(default=False)
    created_at = models.DateTimeField(auto_now_add=True)
    updated_at = models.DateTimeField(auto_now=True)
    
    class Meta:
        db_table = 'config_entries'
        verbose_name = 'Config Entry'
        verbose_name_plural = 'Config Entries'
        unique_together = [['service', 'environment', 'key']]
        indexes = [
            models.Index(fields=['service', 'environment']),
        ]
    
    def __str__(self):
        return f"{self.service}/{self.environment}/{self.key}"


class FeatureFlag(models.Model):
    """
    Feature flag for controlling features
    """
    id = models.UUIDField(primary_key=True, default=uuid.uuid4, editable=False)
    name = models.CharField(max_length=120, unique=True, db_index=True)
    description = models.TextField(blank=True, null=True)
    is_enabled = models.BooleanField(default=False)
    created_at = models.DateTimeField(auto_now_add=True)
    updated_at = models.DateTimeField(auto_now=True)
    deleted_at = models.DateTimeField(blank=True, null=True)
    
    class Meta:
        db_table = 'feature_flags'
        verbose_name = 'Feature Flag'
        verbose_name_plural = 'Feature Flags'
    
    def __str__(self):
        return self.name


class Activity(models.Model):
    """
    Activity log for tracking all system actions
    """
    TYPE_CHOICES = [
        ('create', 'Create'),
        ('update', 'Update'),
        ('delete', 'Delete'),
        ('read', 'Read'),
        ('toggle', 'Toggle'),
        ('login', 'Login'),
        ('logout', 'Logout'),
    ]
    
    RESOURCE_CHOICES = [
        ('user', 'User'),
        ('config', 'Config'),
        ('client', 'Service Client'),
        ('flag', 'Feature Flag'),
        ('oauth_provider', 'OAuth Provider'),
    ]
    
    id = models.UUIDField(primary_key=True, default=uuid.uuid4, editable=False)
    type = models.CharField(max_length=20, choices=TYPE_CHOICES, db_index=True)
    resource = models.CharField(max_length=50, choices=RESOURCE_CHOICES, db_index=True)
    resource_id = models.CharField(max_length=255, db_index=True)  # Changed to CharField to support any ID format
    resource_name = models.CharField(max_length=255, blank=True, null=True)  # Human-readable name
    user_email = models.EmailField(blank=True, null=True, db_index=True)  # Store email for audit trail
    details = models.TextField(blank=True, null=True)  # Additional details (JSON or plain text)
    ip_address = models.GenericIPAddressField(blank=True, null=True)  # Track IP for security
    timestamp = models.DateTimeField(auto_now_add=True, db_index=True)
    
    class Meta:
        db_table = 'activities'
        verbose_name = 'Activity'
        verbose_name_plural = 'Activities'
        ordering = ['-timestamp']
        indexes = [
            models.Index(fields=['-timestamp', 'resource']),
            models.Index(fields=['user_email', '-timestamp']),
        ]
    
    def __str__(self):
        action = f"{self.get_type_display()} {self.get_resource_display()}"
        if self.resource_name:
            action += f" '{self.resource_name}'"
        if self.user_email:
            action += f" by {self.user_email}"
        return action

