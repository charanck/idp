"""
Web UI URL configuration
"""
from django.urls import path
from . import views

app_name = 'web_ui'

urlpatterns = [
    # Home
    path('', views.home, name='home'),
    
    # Authentication
    path('login/', views.login_view, name='login'),
    path('logout/', views.logout_view, name='logout'),
    path('register/', views.register_view, name='register'),
    path('password-change/', views.password_change_view, name='password_change'),
    
    # Dashboard
    path('dashboard/', views.dashboard, name='dashboard'),
    
    # Users management
    path('users/', views.users_list, name='users_list'),
    path('users/create/', views.user_create, name='user_create'),
    path('users/<uuid:pk>/edit/', views.user_edit, name='user_edit'),
    path('users/<uuid:pk>/delete/', views.user_delete, name='user_delete'),
    
    # Service Clients
    path('clients/', views.clients_list, name='clients_list'),
    path('clients/create/', views.client_create, name='client_create'),
    path('clients/<uuid:pk>/', views.client_detail, name='client_detail'),
    path('clients/<uuid:pk>/toggle/', views.client_toggle, name='client_toggle'),

    # Applications
    path('applications/', views.applications_list, name='applications_list'),
    path('applications/create/', views.application_create, name='application_create'),
    path('applications/<uuid:pk>/edit/', views.application_edit, name='application_edit'),
    path('applications/<uuid:pk>/delete/', views.application_delete, name='application_delete'),

    # Environments
    path('environments/', views.environments_list, name='environments_list'),
    path('environments/create/', views.environment_create, name='environment_create'),
    path('environments/<uuid:pk>/edit/', views.environment_edit, name='environment_edit'),
    path('environments/<uuid:pk>/delete/', views.environment_delete, name='environment_delete'),
    
    # Configs (includes secrets via is_secret field)
    path('configs/', views.configs_list, name='configs_list'),
    path('configs/create/', views.config_create, name='config_create'),
    path('configs/<uuid:pk>/clone/', views.config_clone, name='config_clone'),
    path('configs/<uuid:pk>/edit/', views.config_edit, name='config_edit'),
    path('configs/<uuid:pk>/delete/', views.config_delete, name='config_delete'),
    path('configs/<uuid:pk>/history/', views.config_history, name='config_history'),
    path('configs/<uuid:pk>/rollback/<int:version>/', views.config_rollback, name='config_rollback'),
    
    # Feature Flags
    path('flags/', views.flags_list, name='flags_list'),
    path('flags/create/', views.flag_create, name='flag_create'),
    path('flags/<uuid:pk>/toggle/', views.flag_toggle, name='flag_toggle'),
    path('flags/<uuid:pk>/delete/', views.flag_delete, name='flag_delete'),
    
    # OAuth Providers
    path('oauth-providers/', views.oauth_providers_list, name='oauth_providers_list'),
    path('oauth-providers/create/', views.oauth_provider_create, name='oauth_provider_create'),
    path('oauth-providers/<uuid:pk>/edit/', views.oauth_provider_edit, name='oauth_provider_edit'),
    path('oauth-providers/<uuid:pk>/delete/', views.oauth_provider_delete, name='oauth_provider_delete'),
    path('oauth-providers/<uuid:pk>/toggle/', views.oauth_provider_toggle, name='oauth_provider_toggle'),
    
    # OAuth Login Flow
    path('oauth/login/<uuid:provider_id>/', views.oauth_login, name='oauth_login'),
    path('oauth/callback/<uuid:provider_id>/', views.oauth_callback, name='oauth_callback'),
    
    # Activity Log (Readonly)
    path('activity/', views.activity_log, name='activity_log'),
]
