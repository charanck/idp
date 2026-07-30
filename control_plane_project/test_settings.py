"""
Settings used when running the test suite.

Forces a throwaway sqlite database and fast, deterministic crypto settings
regardless of whatever DB_* / MASTER_ENCRYPTION_KEY values are configured in
the developer's .env for the real dev server.
"""
import os

from control_plane_project.settings import *  # noqa: F401,F403

# authentication.apps runs a post_migrate hook that auto-creates an admin
# user from these env vars. Tests must not depend on (or collide with)
# whatever a developer's local .env happens to set, so strip them out here -
# after settings.py's load_dotenv() has already populated os.environ from
# .env, and before the test database's migrations run the hook.
os.environ.pop('ADMIN_EMAIL', None)
os.environ.pop('ADMIN_PASSWORD', None)
os.environ.pop('DJANGO_ADMIN_EMAIL', None)
os.environ.pop('DJANGO_ADMIN_PASSWORD', None)

DATABASES = {
    'default': {
        'ENGINE': 'django.db.backends.sqlite3',
        'NAME': ':memory:',
    }
}

MASTER_ENCRYPTION_KEY = 'txTk-qLL_b3InwMB6S-tfi9r_XyFBZmMsHDJa85B9UU='
JWT_SECRET_KEY = 'test-jwt-secret'

CACHES = {
    'default': {
        'BACKEND': 'django.core.cache.backends.locmem.LocMemCache',
        'LOCATION': 'test-cache',
    },
    'ratelimit': {
        'BACKEND': 'django.core.cache.backends.locmem.LocMemCache',
        'LOCATION': 'test-ratelimit-cache',
    },
}

# High enough that ordinary test traffic never trips the limiter.
AUTH_RATE_LIMIT = 100000
AUTH_RATE_LIMIT_WINDOW_SECONDS = 60

PASSWORD_HASHERS = [
    'django.contrib.auth.hashers.MD5PasswordHasher',
]
