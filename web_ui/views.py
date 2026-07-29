"""
Web UI views with full CRUD functionality
"""
from django.shortcuts import render, redirect, get_object_or_404
from django.contrib.auth import login, logout, authenticate, update_session_auth_hash
from django.contrib.auth.decorators import login_required
from django.contrib import messages
from django.core.paginator import Paginator
from django.db.models import Q

from authentication.models import User, ServiceClient
from authentication.oauth_models import OAuthProvider
from authentication.services import AuthService
from authentication.oauth_service import OAuthService
from config_management.models import ConfigEntry, FeatureFlag, Activity
from config_management.services import ConfigService, FeatureFlagService
from common.activity_logger import log_create, log_update, log_delete, log_toggle, log_login, log_logout

from .forms import (
    LoginForm, 
    UserRegisterForm, 
    UserEditForm,
    ServiceClientForm,
    ConfigEntryForm,
    FeatureFlagForm,
    OAuthProviderForm
,
)

auth_service = AuthService()
config_service = ConfigService()
flag_service = FeatureFlagService()
oauth_service = OAuthService()


# Home
def home(request):
    """Redirect to dashboard if logged in, else to login"""
    if request.user.is_authenticated:
        return redirect('web_ui:dashboard')
    return redirect('web_ui:login')


# Authentication Views
def login_view(request):
    """User login"""
    if request.user.is_authenticated:
        return redirect('web_ui:dashboard')
    
    # Get active OAuth providers (all support authorization code flow for users)
    oauth_providers = OAuthProvider.objects.filter(is_active=True)
    
    if request.method == 'POST':
        form = LoginForm(request, data=request.POST)
        if form.is_valid():
            user = form.get_user()
            login(request, user)
            
            # Check if user needs to reset password
            if user.force_password_reset:
                messages.warning(
                    request, 
                    'You must change your password before continuing.'
                )
                return redirect('web_ui:password_change')
            
            messages.success(request, f'Welcome back, {user.email}!')
            log_login(user.email, request, details='Web UI login')
            return redirect('web_ui:dashboard')
        else:
            messages.error(request, 'Invalid email or password.')
    else:
        form = LoginForm()
    
    return render(request, 'web_ui/login.html', {
        'form': form,
        'oauth_providers': oauth_providers
    })


def register_view(request):
    """User registration"""
    if request.user.is_authenticated:
        return redirect('web_ui:dashboard')
    
    if request.method == 'POST':
        form = UserRegisterForm(request.POST)
        if form.is_valid():
            user = form.save()
            messages.success(request, 'Registration successful! Please log in.')
            return redirect('web_ui:login')
    else:
        form = UserRegisterForm()
    
    return render(request, 'web_ui/register.html', {'form': form})


@login_required
def password_change_view(request):
    """Password change view with force reset support"""
    force_reset = request.user.force_password_reset
    
    if request.method == 'POST':
        old_password = request.POST.get('old_password')
        new_password1 = request.POST.get('new_password1')
        new_password2 = request.POST.get('new_password2')
        
        # Validate passwords
        if not force_reset and not request.user.check_password(old_password):
            messages.error(request, 'Current password is incorrect.')
        elif new_password1 != new_password2:
            messages.error(request, 'New passwords do not match.')
        elif len(new_password1) < 8:
            messages.error(request, 'Password must be at least 8 characters long.')
        else:
            # Change password and clear force reset flag
            request.user.set_password(new_password1)
            request.user.force_password_reset = False
            request.user.save()
            
            # Update session to avoid logout
            update_session_auth_hash(request, request.user)
            
            messages.success(request, 'Password changed successfully!')
            return redirect('web_ui:dashboard')
    
    return render(request, 'web_ui/password_change.html', {
        'force_reset': force_reset
    })


@login_required
def logout_view(request):
    """User logout"""
    user_email = request.user.email
    logout(request)
    log_logout(user_email, request)
    messages.info(request, 'You have been logged out.')
    return redirect('web_ui:login')


# Dashboard
@login_required
def dashboard(request):
    """Main dashboard"""
    # Get counts
    users_count = User.objects.filter(is_active=True).count()
    clients_count = ServiceClient.objects.filter(is_active=True).count()
    configs_count = ConfigEntry.objects.count()
    flags_count = FeatureFlag.objects.filter(deleted_at__isnull=True).count()
    oauth_providers_count = OAuthProvider.objects.filter(is_active=True).count()
    
    # Recent configs
    recent_configs = ConfigEntry.objects.order_by('-created_at')[:5]
    
    context = {
        'users_count': users_count,
        'clients_count': clients_count,
        'configs_count': configs_count,
        'flags_count': flags_count,
        'oauth_providers_count': oauth_providers_count,
        'recent_configs': recent_configs,
    }
    return render(request, 'web_ui/dashboard.html', context)


# Users Management
@login_required
def users_list(request):
    """List all users"""
    search_query = request.GET.get('search', '')
    users = User.objects.all().order_by('-created_at')
    
    if search_query:
        users = users.filter(
            Q(email__icontains=search_query) | 
            Q(username__icontains=search_query)
        )
    
    paginator = Paginator(users, 20)
    page_number = request.GET.get('page')
    page_obj = paginator.get_page(page_number)
    
    return render(request, 'web_ui/users_list.html', {
        'page_obj': page_obj,
        'search_query': search_query
    })


@login_required
def user_create(request):
    """Create new user"""
    if request.method == 'POST':
        form = UserRegisterForm(request.POST)
        if form.is_valid():
            user = form.save()
            log_create('user', str(user.id), user.email, request)
            messages.success(request, f'User {user.email} created successfully.')
            return redirect('web_ui:users_list')
    else:
        form = UserRegisterForm()
    
    return render(request, 'web_ui/user_form.html', {
        'form': form,
        'action': 'Create'
    })


@login_required
def user_edit(request, pk):
    """Edit user"""
    user = get_object_or_404(User, pk=pk)
    
    if request.method == 'POST':
        form = UserEditForm(request.POST, instance=user)
        if form.is_valid():
            form.save()
            log_update('user', str(user.id), user.email, request)
            messages.success(request, f'User {user.email} updated successfully.')
            return redirect('web_ui:users_list')
    else:
        form = UserEditForm(instance=user)
    
    return render(request, 'web_ui/user_form.html', {
        'form': form,
        'action': 'Edit',
        'user': user
    })


@login_required
def user_delete(request, pk):
    """Delete user"""
    user = get_object_or_404(User, pk=pk)
    
    if request.method == 'POST':
        if user == request.user:
            messages.error(request, 'You cannot delete your own account.')
        else:
            email = user.email
            user.delete()
        log_delete('user', str(pk), email, request)
        messages.success(request, f'User {email} deleted successfully.')
        return redirect('web_ui:users_list')
    
    return render(request, 'web_ui/user_confirm_delete.html', {'user': user})


# Service Clients Management
@login_required
def clients_list(request):
    """List all service clients"""
    search_query = request.GET.get('search', '')
    clients = ServiceClient.objects.all().order_by('-created_at')
    
    if search_query:
        clients = clients.filter(name__icontains=search_query)
    
    paginator = Paginator(clients, 20)
    page_number = request.GET.get('page')
    page_obj = paginator.get_page(page_number)
    
    return render(request, 'web_ui/clients_list.html', {
        'page_obj': page_obj,
        'search_query': search_query
    })


@login_required
def client_create(request):
    """Create new service client"""
    if request.method == 'POST':
        form = ServiceClientForm(request.POST)
        if form.is_valid():
            try:
                credentials = auth_service.create_service_client(form.cleaned_data['name'])
                log_create('client', str(credentials.client.id), credentials.client.name, request)
                messages.success(request, f'Service client created successfully.')
                return render(request, 'web_ui/client_created.html', {
                    'client': credentials.client,
                    'api_key': credentials.api_key
                })
            except ValueError as e:
                messages.error(request, str(e))
    else:
        form = ServiceClientForm()
    
    return render(request, 'web_ui/client_form.html', {'form': form})


@login_required
def client_detail(request, pk):
    """View service client details"""
    client = get_object_or_404(ServiceClient, pk=pk)
    return render(request, 'web_ui/client_detail.html', {'client': client})


@login_required
def client_toggle(request, pk):
    """Toggle service client active status"""
    client = get_object_or_404(ServiceClient, pk=pk)
    client.is_active = not client.is_active
    client.save()
    log_toggle('client', str(client.id), client.name, request, details={'is_active': client.is_active})
    
    status = 'activated' if client.is_active else 'deactivated'
    messages.success(request, f'Service client {client.name} {status}.')
    return redirect('web_ui:clients_list')


# Configs Management (includes secrets via is_secret field)
@login_required
def configs_list(request):
    """List all configs and secrets"""
    service_filter = request.GET.get('service', '')
    env_filter = request.GET.get('environment', '')
    search_query = request.GET.get('search', '')
    type_filter = request.GET.get('type', '')  # 'config', 'secret', or empty for all
    
    configs = ConfigEntry.objects.all().order_by('service', 'environment', 'key')
    
    if service_filter:
        configs = configs.filter(service=service_filter)
    if env_filter:
        configs = configs.filter(environment=env_filter)
    if search_query:
        configs = configs.filter(key__icontains=search_query)
    if type_filter == 'secret':
        configs = configs.filter(is_secret=True)
    elif type_filter == 'config':
        configs = configs.filter(is_secret=False)
    
    # Get unique services and environments for filters
    services = ConfigEntry.objects.values_list('service', flat=True).distinct()
    environments = ConfigEntry.objects.values_list('environment', flat=True).distinct()
    
    paginator = Paginator(configs, 20)
    page_number = request.GET.get('page')
    page_obj = paginator.get_page(page_number)
    
    return render(request, 'web_ui/configs_list.html', {
        'page_obj': page_obj,
        'services': services,
        'environments': environments,
        'service_filter': service_filter,
        'env_filter': env_filter,
        'search_query': search_query,
        'type_filter': type_filter,
    })


@login_required
def config_create(request):
    """Create new config"""
    if request.method == 'POST':
        form = ConfigEntryForm(request.POST)
        if form.is_valid():
            config = form.save()
            config_name = f"{config.service}/{config.environment}/{config.key}"
            log_create('config', str(config.id), config_name, request, details={'is_secret': config.is_secret})
            messages.success(request, 'Config created successfully.')
            return redirect('web_ui:configs_list')
    else:
        form = ConfigEntryForm()
    
    return render(request, 'web_ui/config_form.html', {
        'form': form,
        'action': 'Create'
    })


@login_required
def config_edit(request, pk):
    """Edit config"""
    config = get_object_or_404(ConfigEntry, pk=pk)
    
    if request.method == 'POST':
        form = ConfigEntryForm(request.POST, instance=config)
        if form.is_valid():
            config = form.save()
            config_name = f"{config.service}/{config.environment}/{config.key}"
            log_update('config', str(config.id), config_name, request, details={'is_secret': config.is_secret})
            messages.success(request, 'Config updated successfully.')
            return redirect('web_ui:configs_list')
    else:
        form = ConfigEntryForm(instance=config)
    
    return render(request, 'web_ui/config_form.html', {
        'form': form,
        'action': 'Edit',
        'config': config
    })


@login_required
def config_delete(request, pk):
    """Delete config"""
    config = get_object_or_404(ConfigEntry, pk=pk)
    
    if request.method == 'POST':
        config_key = config.key
        config_name = f"{config.service}/{config.environment}/{config.key}"
        config.delete()
        log_delete('config', str(pk), config_name, request)
        messages.success(request, f'Config {config_key} deleted successfully.')
        return redirect('web_ui:configs_list')
    
    return render(request, 'web_ui/config_confirm_delete.html', {'config': config})


# Feature Flags Management
@login_required
def flags_list(request):
    """List all feature flags"""
    flags = FeatureFlag.objects.filter(deleted_at__isnull=True).order_by('name')
    
    search_query = request.GET.get('search', '')
    if search_query:
        flags = flags.filter(name__icontains=search_query)
    
    paginator = Paginator(flags, 20)
    page_number = request.GET.get('page')
    page_obj = paginator.get_page(page_number)
    
    return render(request, 'web_ui/flags_list.html', {
        'page_obj': page_obj,
        'search_query': search_query
    })


@login_required
def flag_create(request):
    """Create new feature flag"""
    if request.method == 'POST':
        form = FeatureFlagForm(request.POST)
        if form.is_valid():
            try:
                flag = flag_service.create_flag(
                    name=form.cleaned_data['name'],
                    description=form.cleaned_data.get('description', ''),
                    is_enabled=form.cleaned_data.get('is_enabled', False)
                )
                log_create('flag', str(flag.id), flag.name, request, details={'is_enabled': flag.is_enabled})
                messages.success(request, 'Feature flag created successfully.')
                return redirect('web_ui:flags_list')
            except ValueError as e:
                messages.error(request, str(e))
    else:
        form = FeatureFlagForm()
    
    return render(request, 'web_ui/flag_form.html', {'form': form})


@login_required
def flag_toggle(request, pk):
    """Toggle feature flag"""
    flag = get_object_or_404(FeatureFlag, pk=pk, deleted_at__isnull=True)
    flag.is_enabled = not flag.is_enabled
    flag.save()
    log_toggle('flag', str(flag.id), flag.name, request, details={'is_enabled': flag.is_enabled})
    
    status = 'enabled' if flag.is_enabled else 'disabled'
    messages.success(request, f'Feature flag {flag.name} {status}.')
    return redirect('web_ui:flags_list')


@login_required
def flag_delete(request, pk):
    """Delete feature flag"""
    from django.utils import timezone
    flag = get_object_or_404(FeatureFlag, pk=pk, deleted_at__isnull=True)
    
    if request.method == 'POST':
        flag_name = flag.name
        flag.deleted_at = timezone.now()
        flag.save()
        log_delete('flag', str(pk), flag_name, request)
        messages.success(request, f'Feature flag {flag_name} deleted successfully.')
        return redirect('web_ui:flags_list')
    
    return render(request, 'web_ui/flag_confirm_delete.html', {'flag': flag})


# OAuth Providers Management
@login_required
def oauth_providers_list(request):
    """List all OAuth providers"""
    providers_list = OAuthProvider.objects.all().order_by('-created_at')
    
    # Pagination
    paginator = Paginator(providers_list, 10)
    page = request.GET.get('page', 1)
    providers = paginator.get_page(page)
    
    return render(request, 'web_ui/oauth_providers_list.html', {
        'providers': providers
    })


@login_required
def oauth_provider_create(request):
    """Create new OAuth provider"""
    if request.method == 'POST':
        form = OAuthProviderForm(request.POST)
        if form.is_valid():
            provider = form.save()
            log_create('oauth_provider', str(provider.id), provider.name, request,
                      details={'scope': provider.scope, 'active': provider.is_active})
            messages.success(request, f'OAuth provider "{provider.name}" created successfully.')
            return redirect('web_ui:oauth_providers_list')
    else:
        form = OAuthProviderForm()
    
    return render(request, 'web_ui/oauth_provider_form.html', {
        'form': form,
        'title': 'Create OAuth Provider'
    })


@login_required
def oauth_provider_edit(request, pk):
    """Edit OAuth provider"""
    provider = get_object_or_404(OAuthProvider, id=pk)
    
    if request.method == 'POST':
        form = OAuthProviderForm(request.POST, instance=provider)
        if form.is_valid():
            provider = form.save()
            log_update('oauth_provider', str(provider.id), provider.name, request,
                      details={'scope': provider.scope, 'active': provider.is_active})
            messages.success(request, f'OAuth provider "{provider.name}" updated successfully.')
            return redirect('web_ui:oauth_providers_list')
    else:
        form = OAuthProviderForm(instance=provider)
    
    return render(request, 'web_ui/oauth_provider_form.html', {
        'form': form,
        'title': 'Edit OAuth Provider',
        'provider': provider
    })


@login_required
def oauth_provider_delete(request, pk):
    """Delete OAuth provider"""
    provider = get_object_or_404(OAuthProvider, id=pk)
    
    if request.method == 'POST':
        provider_name = provider.name
        provider.delete()
        log_delete('oauth_provider', str(pk), provider_name, request)
        messages.success(request, f'OAuth provider "{provider_name}" deleted successfully.')
        return redirect('web_ui:oauth_providers_list')
    
    return render(request, 'web_ui/oauth_provider_confirm_delete.html', {
        'provider': provider
    })


@login_required
def oauth_provider_toggle(request, pk):
    """Toggle OAuth provider active status"""
    provider = get_object_or_404(OAuthProvider, id=pk)
    provider.is_active = not provider.is_active
    provider.save()
    log_toggle('oauth_provider', str(provider.id), provider.name, request,
              details={'is_active': provider.is_active})
    
    status = 'activated' if provider.is_active else 'deactivated'
    messages.success(request, f'OAuth provider "{provider.name}" {status}.')
    return redirect('web_ui:oauth_providers_list')


# OAuth Login Flow (for users)
def oauth_login(request, provider_id):
    """Initiate OAuth login"""
    provider = get_object_or_404(OAuthProvider, id=provider_id, is_active=True)
    
    # Check if provider supports authorization code flow
    if provider.supported_flows not in ['authorization_code', 'both']:
        messages.error(request, f'Provider "{provider.name}" does not support user login.')
        return redirect('web_ui:login')
    
    # Build redirect URI
    redirect_uri = request.build_absolute_uri(f'/oauth/callback/{provider_id}/')
    
    # Get authorization URL
    authorization_url, state = oauth_service.get_authorization_url(provider, redirect_uri)
    
    # Store state in session
    request.session[f'oauth_state_{provider_id}'] = state
    request.session[f'oauth_provider_{provider_id}'] = str(provider.id)
    
    return redirect(authorization_url)


def oauth_callback(request, provider_id):
    """OAuth callback handler"""
    provider = get_object_or_404(OAuthProvider, id=provider_id)
    
    # Get code from callback
    code = request.GET.get('code')
    error = request.GET.get('error')
    
    if error:
        messages.error(request, f'OAuth error: {error}')
        return redirect('web_ui:login')
    
    if not code:
        messages.error(request, 'No authorization code received.')
        return redirect('web_ui:login')
    
    try:
        # Build redirect URI
        redirect_uri = request.build_absolute_uri(f'/oauth/callback/{provider_id}/')
        
        # Exchange code for token
        token_data = oauth_service.exchange_code_for_token(provider, code, redirect_uri)
        
        # Get user info
        user_info = oauth_service.get_user_info(provider, token_data['access_token'])
        
        # Authenticate or create user
        user, oauth_token = oauth_service.authenticate_or_create_user(provider, token_data, user_info)
        
        # Log user in
        login(request, user)
        log_login(user.email, request, details=f'OAuth login via {provider.name}')
        
        messages.success(request, f'Successfully logged in with {provider.name}!')
        return redirect('web_ui:dashboard')
        
    except Exception as e:
        messages.error(request, f'OAuth login failed: {str(e)}')
        return redirect('web_ui:login')


# Activity Log (Readonly)
@login_required
def activity_log(request):
    """View activity log (readonly)"""
    # Get filter parameters
    resource_filter = request.GET.get('resource', '')
    type_filter = request.GET.get('type', '')
    user_filter = request.GET.get('user', '')
    
    # Base queryset
    activities = Activity.objects.all()
    
    # Apply filters
    if resource_filter:
        activities = activities.filter(resource=resource_filter)
    if type_filter:
        activities = activities.filter(type=type_filter)
    if user_filter:
        activities = activities.filter(user_email__icontains=user_filter)
    
    # Pagination
    paginator = Paginator(activities, 50)  # 50 activities per page
    page = request.GET.get('page', 1)
    activities_page = paginator.get_page(page)
    
    # Get distinct resource types and action types for filters
    resource_types = Activity.objects.values_list('resource', flat=True).distinct()
    action_types = Activity.objects.values_list('type', flat=True).distinct()
    
    return render(request, 'web_ui/activity_log.html', {
        'activities': activities_page,
        'resource_types': resource_types,
        'action_types': action_types,
        'current_resource': resource_filter,
        'current_type': type_filter,
        'current_user': user_filter,
    })

