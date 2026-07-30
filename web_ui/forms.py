"""
Django forms for web UI
"""
from django import forms
from django.contrib.auth.forms import AuthenticationForm, UserCreationForm

from authentication.models import ServiceClient, User
from authentication.oauth_models import OAuthProvider
from config_management.models import Application, ConfigEntry, Environment, FeatureFlag


class LoginForm(AuthenticationForm):
    """Login form"""

    username = forms.EmailField(
        widget=forms.EmailInput(
            attrs={
                "class": "form-control",
                "placeholder": "Email",
            }
        )
    )
    password = forms.CharField(
        widget=forms.PasswordInput(
            attrs={
                "class": "form-control",
                "placeholder": "Password",
            }
        )
    )


class UserRegisterForm(UserCreationForm):
    """User registration form"""

    email = forms.EmailField(widget=forms.EmailInput(attrs={"class": "form-control"}))

    class Meta:
        model = User
        fields = ["email", "username", "password1", "password2"]
        widgets = {
            "username": forms.TextInput(attrs={"class": "form-control"}),
        }

    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self.fields["password1"].widget.attrs.update({"class": "form-control"})
        self.fields["password2"].widget.attrs.update({"class": "form-control"})


class UserEditForm(forms.ModelForm):
    """User edit form"""

    class Meta:
        model = User
        fields = ["email", "username", "is_active", "is_staff"]
        widgets = {
            "email": forms.EmailInput(attrs={"class": "form-control"}),
            "username": forms.TextInput(attrs={"class": "form-control"}),
            "is_active": forms.CheckboxInput(attrs={"class": "form-check-input"}),
            "is_staff": forms.CheckboxInput(attrs={"class": "form-check-input"}),
        }


class ServiceClientForm(forms.ModelForm):
    """Service client form"""

    class Meta:
        model = ServiceClient
        fields = ["name"]
        widgets = {
            "name": forms.TextInput(
                attrs={
                    "class": "form-control",
                    "placeholder": "Client name",
                }
            ),
        }


class ApplicationForm(forms.ModelForm):
    """Application form"""

    class Meta:
        model = Application
        fields = ["name"]
        widgets = {
            "name": forms.TextInput(
                attrs={
                    "class": "form-control",
                    "placeholder": "Application name",
                }
            ),
        }


class EnvironmentForm(forms.ModelForm):
    """Environment form"""

    class Meta:
        model = Environment
        fields = ["application", "name"]
        widgets = {
            "application": forms.Select(attrs={"class": "form-select"}),
            "name": forms.TextInput(
                attrs={
                    "class": "form-control",
                    "placeholder": "Environment name (e.g. dev, staging, prod)",
                }
            ),
        }


class ConfigEntryForm(forms.ModelForm):
    """Config entry form"""

    class Meta:
        model = ConfigEntry
        fields = ["application", "environment", "key", "value", "type", "is_secret"]
        widgets = {
            "application": forms.Select(attrs={"class": "form-select"}),
            "environment": forms.Select(attrs={"class": "form-select"}),
            "key": forms.TextInput(attrs={"class": "form-control"}),
            "value": forms.Textarea(attrs={"class": "form-control", "rows": 3}),
            "type": forms.Select(attrs={"class": "form-select"}),
            "is_secret": forms.CheckboxInput(attrs={"class": "form-check-input"}),
        }

    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self.fields["application"].queryset = Application.objects.all().order_by("name")
        self.fields["environment"].queryset = Environment.objects.none()

        selected_application_id = None
        if self.instance and self.instance.pk:
            selected_application_id = self.instance.application_id
        else:
            selected_application_id = self.data.get("application")

        if selected_application_id:
            self.fields["environment"].queryset = Environment.objects.filter(
                application_id=selected_application_id
            ).order_by("name")
        elif not self.is_bound:
            first_application = Application.objects.order_by("name").first()
            if first_application:
                self.fields["application"].initial = first_application.id
                self.fields["environment"].queryset = Environment.objects.filter(
                    application=first_application
                ).order_by("name")

    def clean(self):
        cleaned_data = super().clean()
        application = cleaned_data.get("application")
        environment = cleaned_data.get("environment")

        if application and environment and environment.application_id != application.id:
            self.add_error("environment", "Selected environment does not belong to selected application.")

        return cleaned_data


class FeatureFlagForm(forms.ModelForm):
    """Feature flag form"""

    class Meta:
        model = FeatureFlag
        fields = ["name", "description", "is_enabled"]
        widgets = {
            "name": forms.TextInput(attrs={"class": "form-control"}),
            "description": forms.Textarea(attrs={"class": "form-control", "rows": 2}),
            "is_enabled": forms.CheckboxInput(attrs={"class": "form-check-input"}),
        }


class OAuthProviderForm(forms.ModelForm):
    """Form for OAuth provider configuration"""

    class Meta:
        model = OAuthProvider
        fields = [
            "name",
            "client_id",
            "client_secret",
            "authorization_url",
            "token_url",
            "userinfo_url",
            "scope",
            "auto_create_users",
            "is_active",
        ]
        widgets = {
            "name": forms.TextInput(attrs={"class": "form-control", "placeholder": "Google"}),
            "client_id": forms.TextInput(
                attrs={"class": "form-control", "placeholder": "your-client-id"}
            ),
            "client_secret": forms.PasswordInput(
                attrs={"class": "form-control", "placeholder": "your-client-secret"}
            ),
            "authorization_url": forms.URLInput(
                attrs={
                    "class": "form-control",
                    "placeholder": "https://provider.com/oauth/authorize",
                }
            ),
            "token_url": forms.URLInput(
                attrs={
                    "class": "form-control",
                    "placeholder": "https://provider.com/oauth/token",
                }
            ),
            "userinfo_url": forms.URLInput(
                attrs={
                    "class": "form-control",
                    "placeholder": "https://provider.com/oauth/userinfo",
                }
            ),
            "scope": forms.TextInput(
                attrs={"class": "form-control", "placeholder": "openid email profile"}
            ),
            "auto_create_users": forms.CheckboxInput(attrs={"class": "form-check-input"}),
            "is_active": forms.CheckboxInput(attrs={"class": "form-check-input"}),
        }
