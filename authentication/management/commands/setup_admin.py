"""
Django management command to set up initial admin user
"""
import os
from django.core.management.base import BaseCommand
from django.db import transaction
from authentication.models import User


class Command(BaseCommand):
    help = 'Set up initial admin user from environment variable'

    def add_arguments(self, parser):
        parser.add_argument(
            '--email',
            type=str,
            help='Admin email (defaults to ADMIN_EMAIL environment variable)',
        )
        parser.add_argument(
            '--password',
            type=str,
            help='Initial admin password (defaults to ADMIN_PASSWORD environment variable)',
        )
        parser.add_argument(
            '--no-force-reset',
            action='store_true',
            help='Do not force password reset on first login',
        )

    @transaction.atomic
    def handle(self, *args, **options):
        email = options.get('email') or os.getenv('ADMIN_EMAIL')
        password = options.get('password') or os.getenv('ADMIN_PASSWORD', 'admin123')
        force_reset = not options.get('no_force_reset', False)

        if not email:
            self.stdout.write(
                self.style.ERROR(
                    'Admin email not provided. Set ADMIN_EMAIL environment variable '
                    'or use --email flag.'
                )
            )
            return

        # Check if admin user already exists
        if User.objects.filter(email=email).exists():
            user = User.objects.get(email=email)
            self.stdout.write(
                self.style.WARNING(
                    f'Admin user with email {email} already exists.'
                )
            )
            
            # Update user to be superuser if not already
            updated = False
            if not user.is_superuser or not user.is_staff:
                user.is_superuser = True
                user.is_staff = True
                user.is_active = True
                updated = True

            if password:
                user.set_password(password)
                updated = True

            user.force_password_reset = force_reset
            user.save()

            if updated:
                self.stdout.write(
                    self.style.SUCCESS(
                        f'Updated existing user {email} to superuser and synced credentials.'
                    )
                )
            return

        # Create admin user
        username = email.split('@')[0]
        user = User.objects.create(
            email=email,
            username=username,
            is_superuser=True,
            is_staff=True,
            is_active=True,
            force_password_reset=force_reset,
        )
        user.set_password(password)
        user.save()

        self.stdout.write(
            self.style.SUCCESS(
                f'Successfully created admin user: {email}'
            )
        )
        
        if force_reset:
            self.stdout.write(
                self.style.WARNING(
                    f'User will be required to reset password on first login.'
                )
            )
        
        self.stdout.write(
            self.style.SUCCESS(
                f'\nAdmin credentials:\n'
                f'  Email: {email}\n'
                f'  Password: {password}\n'
                f'\nWARNING: Please change the password immediately after first login!'
            )
        )
