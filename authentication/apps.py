import os
import sys

from django.apps import AppConfig
from django.contrib.auth import get_user_model
from django.db.models.signals import post_migrate


def create_initial_admin_user(sender, **kwargs):
    # Only run for authentication app migrations
    if sender.label != 'authentication':
        return

    email = os.getenv('ADMIN_EMAIL') or os.getenv('DJANGO_ADMIN_EMAIL')
    if not email:
        print("[AdminSetup] No ADMIN_EMAIL or DJANGO_ADMIN_EMAIL provided, skipping admin creation", file=sys.stderr)
        return

    admin_password = os.getenv('ADMIN_PASSWORD') or os.getenv('DJANGO_ADMIN_PASSWORD')
    if not admin_password:
        print("[AdminSetup] No ADMIN_PASSWORD or DJANGO_ADMIN_PASSWORD provided, skipping admin creation", file=sys.stderr)
        return

    User = get_user_model()

    try:
        user = User.objects.get(email=email)
        print(f"[AdminSetup] User {email} already exists, updating...", file=sys.stderr)
    except User.DoesNotExist:
        print(f"[AdminSetup] Creating new admin user: {email}", file=sys.stderr)
        user = User.objects.create(
            email=email,
            username=email.split('@')[0],
            is_active=True,
            is_staff=True,
            is_superuser=True,
            force_password_reset=True,
        )

    # Ensure user has admin privileges
    user.is_active = True
    user.is_staff = True
    user.is_superuser = True
    user.force_password_reset = True
    user.username = user.username or email.split('@')[0]
    user.set_password(admin_password)
    user.save()
    
    print(f"[AdminSetup] Admin user {email} created/updated successfully", file=sys.stderr)


class AuthenticationConfig(AppConfig):
    name = 'authentication'

    def ready(self):
        post_migrate.connect(
            create_initial_admin_user,
            sender=self,
            dispatch_uid='authentication.create_initial_admin_user',
        )
