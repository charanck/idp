"""
URL configuration for control_plane_project project.
"""
from django.contrib import admin
from django.urls import path, include
from ninja import NinjaAPI

from authentication.api import router as auth_router
from config_management.api import router as config_router

# Create Ninja API instance
api = NinjaAPI(
    title="Control Plane API",
    description="Simple config/secret management with user and S2S authentication",
    version="1.0.0",
)

# Register API routers
api.add_router("/auth/", auth_router)
api.add_router("/config/", config_router)

urlpatterns = [
    path('admin/', admin.site.urls),
    path('api/', api.urls),
    path('', include('web_ui.urls')),  # Web UI routes
]
