"""
URL configuration for control_plane_project project.
"""
import logging

from django.contrib import admin
from django.http import HttpRequest
from django.urls import path, include
from ninja import NinjaAPI

from authentication.api import router as auth_router
from config_management.api import router as config_router

logger = logging.getLogger(__name__)

# Create Ninja API instance
api = NinjaAPI(
    title="Control Plane API",
    description="Simple config/secret management with user and S2S authentication",
    version="1.0.0",
)

# Register API routers
api.add_router("/auth/", auth_router)
api.add_router("/config/", config_router)


@api.exception_handler(Exception)
def handle_unexpected_error(request: HttpRequest, exc: Exception):
    """
    Catch-all for anything endpoints don't handle themselves.

    Without this, an unhandled exception either leaks a Django debug
    traceback (DEBUG=True) or returns Django's generic HTML 500 page
    (DEBUG=False) instead of the JSON clients expect - and neither path
    gets logged with request context.
    """
    logger.exception("Unhandled exception on %s %s", request.method, request.path)
    return api.create_response(
        request,
        {"detail": "Internal server error"},
        status=500,
    )

urlpatterns = [
    path('admin/', admin.site.urls),
    path('api/v1/', api.urls),
    path('', include('web_ui.urls')),  # Web UI routes
]
