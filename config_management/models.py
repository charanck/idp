"""
Configuration Management models
"""
import uuid
from django.core.exceptions import ValidationError
from django.db import models


class Application(models.Model):
    """Application/service container for configurations."""

    id = models.UUIDField(primary_key=True, default=uuid.uuid4, editable=False)
    name = models.CharField(max_length=120, unique=True, db_index=True)
    created_at = models.DateTimeField(auto_now_add=True)
    updated_at = models.DateTimeField(auto_now=True)

    class Meta:
        db_table = "applications"
        verbose_name = "Application"
        verbose_name_plural = "Applications"
        ordering = ["name"]

    def __str__(self):
        return self.name


class Environment(models.Model):
    """Environment scoped to a specific application."""

    id = models.UUIDField(primary_key=True, default=uuid.uuid4, editable=False)
    application = models.ForeignKey(
        Application,
        on_delete=models.CASCADE,
        related_name="environments",
    )
    name = models.CharField(max_length=120, db_index=True)
    created_at = models.DateTimeField(auto_now_add=True)
    updated_at = models.DateTimeField(auto_now=True)

    class Meta:
        db_table = "environments"
        verbose_name = "Environment"
        verbose_name_plural = "Environments"
        unique_together = [["application", "name"]]
        indexes = [
            models.Index(fields=["application", "name"]),
        ]
        ordering = ["application__name", "name"]

    def __str__(self):
        return f"{self.application.name}/{self.name}"


class ConfigEntry(models.Model):
    """
    Configuration entry for an application and environment
    """

    TYPE_CHOICES = [
        ("boolean", "Boolean"),
        ("string", "String"),
        ("number", "Number"),
        ("object", "Object"),
        ("array", "Array"),
    ]

    id = models.UUIDField(primary_key=True, default=uuid.uuid4, editable=False)
    application = models.ForeignKey(
        Application,
        on_delete=models.CASCADE,
        related_name="config_entries",
    )
    environment = models.ForeignKey(
        Environment,
        on_delete=models.CASCADE,
        related_name="config_entries",
    )
    key = models.CharField(max_length=120)
    value = models.TextField()
    type = models.CharField(max_length=20, choices=TYPE_CHOICES, default="string")
    is_secret = models.BooleanField(default=False)
    created_at = models.DateTimeField(auto_now_add=True)
    updated_at = models.DateTimeField(auto_now=True)

    class Meta:
        db_table = "config_entries"
        verbose_name = "Config Entry"
        verbose_name_plural = "Config Entries"
        unique_together = [["application", "environment", "key"]]
        indexes = [
            models.Index(fields=["application", "environment"]),
        ]

    def clean(self):
        if (
            self.application_id
            and self.environment_id
            and self.environment.application_id != self.application_id
        ):
            raise ValidationError("Selected environment does not belong to selected application.")

    @property
    def service_name(self):
        return self.application.name

    @property
    def environment_name(self):
        return self.environment.name

    def __str__(self):
        return f"{self.application.name}/{self.environment.name}/{self.key}"


class FeatureFlag(models.Model):
    """
    Feature flag for controlling features
    """

    id = models.UUIDField(primary_key=True, default=uuid.uuid4, editable=False)
    application = models.ForeignKey(
        Application,
        on_delete=models.CASCADE,
        related_name="feature_flags",
    )
    environment = models.ForeignKey(
        Environment,
        on_delete=models.CASCADE,
        related_name="feature_flags",
    )
    name = models.CharField(max_length=120, db_index=True)
    description = models.TextField(blank=True, null=True)
    is_enabled = models.BooleanField(default=False)
    created_at = models.DateTimeField(auto_now_add=True)
    updated_at = models.DateTimeField(auto_now=True)
    deleted_at = models.DateTimeField(blank=True, null=True)

    class Meta:
        db_table = "feature_flags"
        verbose_name = "Feature Flag"
        verbose_name_plural = "Feature Flags"
        unique_together = [["application", "environment", "name"]]
        indexes = [
            models.Index(fields=["application", "environment", "name"]),
            models.Index(fields=["application", "environment", "deleted_at"]),
            models.Index(fields=["deleted_at"]),
        ]

    def clean(self):
        if (
            self.application_id
            and self.environment_id
            and self.environment.application_id != self.application_id
        ):
            raise ValidationError("Selected environment does not belong to selected application.")

    def __str__(self):
        return f"{self.application.name}/{self.environment.name}/{self.name}"


class Activity(models.Model):
    """
    Activity log for tracking all system actions
    """

    TYPE_CHOICES = [
        ("create", "Create"),
        ("update", "Update"),
        ("delete", "Delete"),
        ("read", "Read"),
        ("toggle", "Toggle"),
        ("login", "Login"),
        ("logout", "Logout"),
    ]

    RESOURCE_CHOICES = [
        ("user", "User"),
        ("config", "Config"),
        ("client", "Service Client"),
        ("flag", "Feature Flag"),
        ("oauth_provider", "OAuth Provider"),
        ("application", "Application"),
        ("environment", "Environment"),
    ]

    id = models.UUIDField(primary_key=True, default=uuid.uuid4, editable=False)
    type = models.CharField(max_length=20, choices=TYPE_CHOICES, db_index=True)
    resource = models.CharField(max_length=50, choices=RESOURCE_CHOICES, db_index=True)
    resource_id = models.CharField(max_length=255, db_index=True)
    resource_name = models.CharField(max_length=255, blank=True, null=True)
    user_email = models.EmailField(blank=True, null=True, db_index=True)
    details = models.TextField(blank=True, null=True)
    ip_address = models.GenericIPAddressField(blank=True, null=True)
    timestamp = models.DateTimeField(auto_now_add=True, db_index=True)

    class Meta:
        db_table = "activities"
        verbose_name = "Activity"
        verbose_name_plural = "Activities"
        ordering = ["-timestamp"]
        indexes = [
            models.Index(fields=["-timestamp", "resource"]),
            models.Index(fields=["user_email", "-timestamp"]),
            models.Index(fields=["resource", "type", "-timestamp"]),
        ]

    def __str__(self):
        action = f"{self.get_type_display()} {self.get_resource_display()}"
        if self.resource_name:
            action += f" '{self.resource_name}'"
        if self.user_email:
            action += f" by {self.user_email}"
        return action
