from django.conf import settings
from django.http import HttpResponse

from common.ratelimit import is_rate_limited


class RateLimitMiddleware:
    """Throttles brute-force-able auth endpoints (token issuance, registration, login) per client IP."""

    # (method, path) -> rate-limit bucket key
    LIMITED_PATHS = {
        ('POST', '/api/v1/auth/token'): 'auth-token',
        ('POST', '/api/v1/auth/register'): 'auth-register',
        ('POST', '/login/'): 'web-login',
    }

    def __init__(self, get_response):
        self.get_response = get_response

    def __call__(self, request):
        bucket = self.LIMITED_PATHS.get((request.method, request.path))
        if bucket:
            limit = settings.AUTH_RATE_LIMIT
            window = settings.AUTH_RATE_LIMIT_WINDOW_SECONDS
            if is_rate_limited(request, bucket, limit, window):
                response = HttpResponse(
                    "Too many requests. Please try again later.",
                    content_type="text/plain",
                    status=429,
                )
                response["Retry-After"] = str(window)
                return response

        return self.get_response(request)
