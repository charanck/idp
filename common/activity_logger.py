"""
Activity logging utility - Simplified
"""
import json
from typing import Optional, Any
from django.http import HttpRequest
from config_management.models import Activity


def get_client_ip(request: HttpRequest) -> Optional[str]:
    """Get client IP address from request"""
    x_forwarded_for = request.META.get('HTTP_X_FORWARDED_FOR')
    if x_forwarded_for:
        return x_forwarded_for.split(',')[0].strip()
    return request.META.get('REMOTE_ADDR')


def log_activity(
    type: str,
    resource: str,
    resource_id: str,
    resource_name: str = None,
    user_email: str = None,
    request: HttpRequest = None,
    details: Any = None
):
    """Log an activity to the database"""
    if not user_email and request and hasattr(request, 'user') and request.user.is_authenticated:
        user_email = request.user.email
    
    ip_address = get_client_ip(request) if request else None
    
    details_str = None
    if details:
        details_str = json.dumps(details) if isinstance(details, (dict, list)) else str(details)
    
    Activity.objects.create(
        type=type,
        resource=resource,
        resource_id=str(resource_id),
        resource_name=resource_name,
        user_email=user_email,
        ip_address=ip_address,
        details=details_str
    )


# Convenience functions
def log_create(resource: str, resource_id: str, resource_name: str = None, request: HttpRequest = None, details: Any = None):
    log_activity('create', resource, resource_id, resource_name, request=request, details=details)


def log_update(resource: str, resource_id: str, resource_name: str = None, request: HttpRequest = None, details: Any = None):
    log_activity('update', resource, resource_id, resource_name, request=request, details=details)


def log_delete(resource: str, resource_id: str, resource_name: str = None, request: HttpRequest = None, details: Any = None):
    log_activity('delete', resource, resource_id, resource_name, request=request, details=details)


def log_toggle(resource: str, resource_id: str, resource_name: str = None, request: HttpRequest = None, details: Any = None):
    log_activity('toggle', resource, resource_id, resource_name, request=request, details=details)


def log_login(user_email: str, request: HttpRequest = None, details: Any = None):
    log_activity('login', 'user', user_email, user_email, user_email=user_email, request=request, details=details)


def log_logout(user_email: str, request: HttpRequest = None):
    log_activity('logout', 'user', user_email, user_email, user_email=user_email, request=request)
