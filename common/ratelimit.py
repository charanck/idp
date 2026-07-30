"""Fixed-window rate limiting, backed by the dedicated 'ratelimit' cache alias (see settings.CACHES)."""
from django.core.cache import caches


def get_client_ip(request) -> str:
    x_forwarded_for = request.META.get('HTTP_X_FORWARDED_FOR')
    if x_forwarded_for:
        return x_forwarded_for.split(',')[0].strip()
    return request.META.get('REMOTE_ADDR', 'unknown')


def is_rate_limited(request, key: str, limit: int, window_seconds: int) -> bool:
    """Allow at most `limit` requests per `window_seconds` per (key, client IP)."""
    cache = caches['ratelimit']
    cache_key = f"ratelimit:{key}:{get_client_ip(request)}"

    try:
        count = cache.incr(cache_key)
    except ValueError:
        cache.set(cache_key, 1, window_seconds)
        count = 1

    return count > limit
