import logging
import os

from django.apps import AppConfig
from django.contrib.auth import get_user_model
from django.db.models.signals import post_migrate

logger = logging.getLogger(__name__)


def create_initial_admin_user(sender, **kwargs):
    # Only run for authentication app migrations
    if sender.label != 'authentication':
        return

    email = os.getenv('ADMIN_EMAIL') or os.getenv('DJANGO_ADMIN_EMAIL')
    if not email:
        logger.info("[AdminSetup] No ADMIN_EMAIL or DJANGO_ADMIN_EMAIL provided, skipping admin creation")
        return

    admin_password = os.getenv('ADMIN_PASSWORD') or os.getenv('DJANGO_ADMIN_PASSWORD')
    if not admin_password:
        logger.info("[AdminSetup] No ADMIN_PASSWORD or DJANGO_ADMIN_PASSWORD provided, skipping admin creation")
        return

    User = get_user_model()

    try:
        try:
            user = User.objects.get(email=email)
            logger.info("[AdminSetup] User %s already exists, updating...", email)
        except User.DoesNotExist:
            logger.info("[AdminSetup] Creating new admin user: %s", email)
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
    except Exception:
        # This runs as a post_migrate hook - a failure here must not abort
        # the migration itself, but it does mean there's no admin access.
        logger.exception("[AdminSetup] Failed to create/update admin user %s", email)
        return

    logger.info("[AdminSetup] Admin user %s created/updated successfully", email)


class AuthenticationConfig(AppConfig):
    name = 'authentication'

    def ready(self):
        post_migrate.connect(
            create_initial_admin_user,
            sender=self,
            dispatch_uid='authentication.create_initial_admin_user',
        )
