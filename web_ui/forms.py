"""
Django forms for web UI
"""
from django import forms
from django.contrib.auth.forms import UserCreationForm, AuthenticationForm
from authentication.models import User, ServiceClient
from authentication.oauth_models import OAuthProvider
from config_management.models import ConfigEntry, FeatureFlag


class LoginForm(AuthenticationForm):
    """Login form"""
    username = forms.EmailField(
        widget=forms.EmailInput(attrs={
            'class': 'form-control',
            'placeholder': 'Email'
        })
    )
    password = forms.CharField(
        widget=forms.PasswordInput(attrs={
            'class': 'form-control',
            'placeholder': 'Password'
        })
    )


class UserRegisterForm(UserCreationForm):
    """User registration form"""
    email = forms.EmailField(
        widget=forms.EmailInput(attrs={'class': 'form-control'})
    )
    
    class Meta:
        model = User
        fields = ['email', 'username', 'password1', 'password2']
        widgets = {
            'username': forms.TextInput(attrs={'class': 'form-control'}),
        }
    
    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self.fields['password1'].widget.attrs.update({'class': 'form-control'})
        self.fields['password2'].widget.attrs.update({'class': 'form-control'})


class UserEditForm(forms.ModelForm):
    """User edit form"""
    class Meta:
        model = User
        fields = ['email', 'username', 'is_active', 'is_staff']
        widgets = {
            'email': forms.EmailInput(attrs={'class': 'form-control'}),
            'username': forms.TextInput(attrs={'class': 'form-control'}),
            'is_active': forms.CheckboxInput(attrs={'class': 'form-check-input'}),
            'is_staff': forms.CheckboxInput(attrs={'class': 'form-check-input'}),
        }


class ServiceClientForm(forms.ModelForm):
    """Service client form"""
    class Meta:
        model = ServiceClient
        fields = ['name']
        widgets = {
            'name': forms.TextInput(attrs={
                'class': 'form-control',
                'placeholder': 'Client name'
            }),
        }


class ConfigEntryForm(forms.ModelForm):
    """Config entry form"""
    class Meta:
        model = ConfigEntry
        fields = ['service', 'environment', 'key', 'value', 'type', 'is_secret']
        widgets = {
            'service': forms.TextInput(attrs={'class': 'form-control'}),
            'environment': forms.TextInput(attrs={'class': 'form-control'}),
            'key': forms.TextInput(attrs={'class': 'form-control'}),
            'value': forms.Textarea(attrs={'class': 'form-control', 'rows': 3}),
            'type': forms.Select(attrs={'class': 'form-select'}),
            'is_secret': forms.CheckboxInput(attrs={'class': 'form-check-input'}),
        }


class FeatureFlagForm(forms.ModelForm):
    """Feature flag form"""
    class Meta:
        model = FeatureFlag
        fields = ['name', 'description', 'is_enabled']
        widgets = {
            'name': forms.TextInput(attrs={'class': 'form-control'}),
            'description': forms.Textarea(attrs={'class': 'form-control', 'rows': 2}),
            'is_enabled': forms.CheckboxInput(attrs={'class': 'form-check-input'}),
        }


class OAuthProviderForm(forms.ModelForm):
    """Form for OAuth provider configuration"""
    
    class Meta:
        model = OAuthProvider
        fields = [
            'name', 'client_id', 'client_secret',
            'authorization_url', 'token_url', 'userinfo_url', 'scope',
            'auto_create_users', 'is_active'
        ]
        widgets = {
            'name': forms.TextInput(attrs={'class': 'form-control', 'placeholder': 'Google'}),
            'client_id': forms.TextInput(attrs={'class': 'form-control', 'placeholder': 'your-client-id'}),
            'client_secret': forms.PasswordInput(attrs={'class': 'form-control', 'placeholder': 'your-client-secret'}),
            'authorization_url': forms.URLInput(attrs={'class': 'form-control', 'placeholder': 'https://provider.com/oauth/authorize'}),
            'token_url': forms.URLInput(attrs={'class': 'form-control', 'placeholder': 'https://provider.com/oauth/token'}),
            'userinfo_url': forms.URLInput(attrs={'class': 'form-control', 'placeholder': 'https://provider.com/oauth/userinfo'}),
            'scope': forms.TextInput(attrs={'class': 'form-control', 'placeholder': 'openid email profile'}),
            'auto_create_users': forms.CheckboxInput(attrs={'class': 'form-check-input'}),
            'is_active': forms.CheckboxInput(attrs={'class': 'form-check-input'}),
        }
