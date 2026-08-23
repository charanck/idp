"""
End-to-end scenario for the control-plane API surface (/api/v1/...).

This drives a real, running server over HTTP - it doesn't care whether that
server is the Django/Ninja app or the Go rewrite in server/, as long as it
speaks the same JSON API contract. That's what makes it usable both as an
E2E test for a single backend and as a parity check between the two: run()
against two base URLs and diff the two ordered result lists.

Each server under test needs its own admin user (ADMIN_EMAIL/ADMIN_PASSWORD)
and its own storage - the scenario never assumes shared state between runs,
every resource name is randomized per run so repeat runs against the same
server don't collide either.
"""
from __future__ import annotations

import time
import uuid
from dataclasses import dataclass, field
from typing import Callable, Optional

import requests
from cryptography.fernet import Fernet


@dataclass
class Check:
    name: str
    ok: bool
    detail: str = ""
    elapsed_ms: Optional[float] = None


@dataclass
class ScenarioConfig:
    base_url: str
    admin_email: str
    admin_password: str
    auth_rate_limit: int = 10
    timeout: float = 10.0


@dataclass
class ScenarioResult:
    checks: list[Check] = field(default_factory=list)

    def add(self, name: str, ok: bool, detail: str = "", elapsed_ms: Optional[float] = None):
        self.checks.append(Check(name, ok, detail, elapsed_ms))

    def as_dict(self):
        return {c.name: (c.ok, c.detail) for c in self.checks}

    @property
    def all_ok(self) -> bool:
        return all(c.ok for c in self.checks)


def _timed(fn: Callable[[], requests.Response]) -> tuple[requests.Response, float]:
    start = time.perf_counter()
    resp = fn()
    elapsed_ms = (time.perf_counter() - start) * 1000
    return resp, elapsed_ms


def run(cfg: ScenarioConfig) -> ScenarioResult:
    r = ScenarioResult()
    s = requests.Session()
    base = cfg.base_url.rstrip("/")
    run_id = uuid.uuid4().hex[:8]

    def check(name, ok, detail="", elapsed_ms=None):
        r.add(name, ok, detail, elapsed_ms)
        return ok

    def post(path, json=None, headers=None):
        return _timed(lambda: s.post(f"{base}{path}", json=json, headers=headers, timeout=cfg.timeout))

    def get(path, params=None, headers=None):
        return _timed(lambda: s.get(f"{base}{path}", params=params, headers=headers, timeout=cfg.timeout))

    # --- admin login -----------------------------------------------------
    resp, ms = post("/api/v1/auth/token", {"username": cfg.admin_email, "password": cfg.admin_password})
    if not check("admin_login", resp.status_code == 200, f"status={resp.status_code} body={resp.text[:200]}", ms):
        return r  # nothing else is reachable without an admin token
    admin_token = resp.json()["access_token"]
    admin_headers = {"Authorization": f"Bearer {admin_token}"}

    resp, ms = get("/api/v1/auth/me", headers=admin_headers)
    check("admin_me", resp.status_code == 200 and resp.json().get("email") == cfg.admin_email,
          f"status={resp.status_code} body={resp.text[:200]}", ms)

    # --- user registration (new users are inactive until an admin activates them) ---
    new_email = f"e2e-user-{run_id}@example.com"
    resp, ms = post("/api/v1/auth/register", {"email": new_email, "password": "TestPass123!"}, headers=admin_headers)
    check("register_user", resp.status_code == 200 and resp.json().get("is_active") is False,
          f"status={resp.status_code} body={resp.text[:200]}", ms)

    resp, ms = post("/api/v1/auth/register", {"email": new_email, "password": "TestPass123!"}, headers=admin_headers)
    check("register_duplicate_conflict", resp.status_code == 409, f"status={resp.status_code}", ms)

    resp, ms = post("/api/v1/auth/token", {"username": new_email, "password": "TestPass123!"})
    check("inactive_user_login_rejected", resp.status_code == 401, f"status={resp.status_code}", ms)

    resp, ms = post("/api/v1/auth/register", {"email": new_email, "password": "x"}, headers=None)
    check("register_requires_admin", resp.status_code in (401, 403), f"status={resp.status_code}", ms)

    # --- service client (S2S) --------------------------------------------
    client_name = f"e2e-client-{run_id}"
    resp, ms = post("/api/v1/auth/s2s/clients", {"name": client_name}, headers=admin_headers)
    if not check("create_service_client", resp.status_code == 200, f"status={resp.status_code} body={resp.text[:200]}", ms):
        return r
    client = resp.json()
    api_key = client["api_key"]
    client_encryption_key = client["encryption_key"]
    api_headers = {"X-API-Key": api_key}

    resp, ms = get("/api/v1/auth/s2s/ping", headers=api_headers)
    check("s2s_ping", resp.status_code == 200 and resp.json().get("ok") is True,
          f"status={resp.status_code} body={resp.text[:200]}", ms)

    resp, ms = get("/api/v1/auth/s2s/ping", headers={"X-API-Key": "bad.key"})
    check("s2s_ping_bad_key_rejected", resp.status_code == 401, f"status={resp.status_code}", ms)

    # --- config entries ----------------------------------------------------
    service = f"e2e-svc-{run_id}"
    environment = "production"
    plain_value = "hello-world"
    secret_value = "super-secret-value"

    resp, ms = post("/api/v1/config/configs/upsert", {
        "service": service, "environment": environment, "key": "GREETING",
        "value": plain_value, "is_secret": False, "type": "string",
    }, headers=admin_headers)
    if not check("upsert_plain_config", resp.status_code == 200 and resp.json().get("value") == "***ENCRYPTED***",
                 f"status={resp.status_code} body={resp.text[:200]}", ms):
        return r
    config_id = resp.json()["id"]

    resp, ms = post("/api/v1/config/configs/upsert", {
        "service": service, "environment": environment, "key": "API_SECRET",
        "value": secret_value, "is_secret": True, "type": "string",
    }, headers=admin_headers)
    check("upsert_secret_config", resp.status_code == 200, f"status={resp.status_code} body={resp.text[:200]}", ms)

    # update the plain config to produce a second version, for history/rollback
    updated_value = "hello-world-v2"
    resp, ms = post("/api/v1/config/configs/upsert", {
        "service": service, "environment": environment, "key": "GREETING",
        "value": updated_value, "is_secret": False, "type": "string",
    }, headers=admin_headers)
    check("update_plain_config", resp.status_code == 200, f"status={resp.status_code}", ms)

    resp, ms = get(f"/api/v1/config/configs/{config_id}/history", headers=admin_headers)
    ok = resp.status_code == 200 and len(resp.json()) >= 2
    versions = resp.json() if ok else []
    check("config_history", ok, f"status={resp.status_code} versions={len(versions)}", ms)

    if ok:
        first_version = min(v["version"] for v in versions)
        resp, ms = post(f"/api/v1/config/configs/{config_id}/rollback", {"version": first_version}, headers=admin_headers)
        check("config_rollback", resp.status_code == 200, f"status={resp.status_code} body={resp.text[:200]}", ms)

    # --- client-side decrypt-and-verify round trip -------------------------
    resp, ms = get("/api/v1/config/configs/list", params={"service": service, "environment": environment}, headers=api_headers)
    ok = resp.status_code == 200
    check("list_configs_for_client", ok, f"status={resp.status_code} body={resp.text[:200]}", ms)
    if ok:
        by_key = {c["key"]: c for c in resp.json()}
        fernet = Fernet(client_encryption_key.encode())
        try:
            decrypted_greeting = fernet.decrypt(by_key["GREETING"]["value"].encode()).decode()
            decrypted_secret = fernet.decrypt(by_key["API_SECRET"]["value"].encode()).decode()
            # rolled back to the first version, so GREETING should be back to plain_value
            check("client_decrypt_plain_value_matches_after_rollback", decrypted_greeting == plain_value,
                  f"got={decrypted_greeting!r} want={plain_value!r}")
            check("client_decrypt_secret_value_matches", decrypted_secret == secret_value,
                  f"got={decrypted_secret!r} want={secret_value!r}")
        except Exception as exc:  # noqa: BLE001 - report any crypto/parse failure as a failed check
            check("client_decrypt_plain_value_matches_after_rollback", False, str(exc))
            check("client_decrypt_secret_value_matches", False, str(exc))
        check("list_configs_never_leaks_plaintext_secret", secret_value not in resp.text,
              "raw response text should never contain the plaintext secret")

    # a client with the wrong key must not be able to decrypt
    wrong_key = Fernet.generate_key().decode()
    resp, ms = get("/api/v1/config/configs/list", params={"service": service, "environment": environment}, headers=api_headers)
    if resp.status_code == 200:
        by_key = {c["key"]: c for c in resp.json()}
        try:
            Fernet(wrong_key.encode()).decrypt(by_key["GREETING"]["value"].encode())
            check("wrong_client_key_cannot_decrypt", False, "decrypt unexpectedly succeeded")
        except Exception:
            check("wrong_client_key_cannot_decrypt", True)

    # --- feature flags -------------------------------------------------------
    flag_name = f"e2e-flag-{run_id}"
    resp, ms = post("/api/v1/config/feature-flags", {
        "service": service, "environment": environment, "name": flag_name,
        "description": "e2e test flag", "is_enabled": False,
    }, headers=admin_headers)
    check("create_feature_flag", resp.status_code == 200 and resp.json().get("is_enabled") is False,
          f"status={resp.status_code} body={resp.text[:200]}", ms)

    resp, ms = get("/api/v1/config/feature-flags", params={"service": service, "environment": environment}, headers=admin_headers)
    check("list_feature_flags_jwt", resp.status_code == 200 and any(f["name"] == flag_name for f in resp.json()),
          f"status={resp.status_code}", ms)

    resp, ms = get("/api/v1/config/feature-flags", params={"service": service, "environment": environment}, headers=api_headers)
    check("list_feature_flags_apikey", resp.status_code == 200 and any(f["name"] == flag_name for f in resp.json()),
          f"status={resp.status_code}", ms)

    # toggle takes service/environment as query params, not a body
    resp, ms = _timed(lambda: s.post(
        f"{base}/api/v1/config/feature-flags/{flag_name}/toggle",
        params={"service": service, "environment": environment},
        headers=admin_headers, timeout=cfg.timeout,
    ))
    check("toggle_feature_flag", resp.status_code == 200 and resp.json().get("is_enabled") is True,
          f"status={resp.status_code} body={resp.text[:200]}", ms)

    resp, ms = _timed(lambda: s.post(
        f"{base}/api/v1/config/feature-flags/does-not-exist/toggle",
        params={"service": service, "environment": environment},
        headers=admin_headers, timeout=cfg.timeout,
    ))
    check("toggle_missing_flag_404", resp.status_code == 404, f"status={resp.status_code}", ms)

    # --- authz edge cases ------------------------------------------------
    resp, ms = get("/api/v1/auth/me", headers={"Authorization": "Bearer garbage"})
    check("invalid_jwt_rejected", resp.status_code == 401, f"status={resp.status_code}", ms)

    resp, ms = post("/api/v1/auth/s2s/clients", {"name": f"e2e-client-{run_id}-2"}, headers=api_headers)
    check("non_admin_cannot_create_client", resp.status_code in (401, 403), f"status={resp.status_code}", ms)

    # --- auth rate limiting -----------------------------------------------
    # Fire enough bad-password attempts in this window to guarantee the
    # configured limit is exceeded regardless of what earlier checks already
    # consumed from the same per-IP bucket.
    attempts = cfg.auth_rate_limit + 5
    saw_429 = False
    last_status = None
    for _ in range(attempts):
        resp, _ = post("/api/v1/auth/token", {"username": cfg.admin_email, "password": "wrong-password"})
        last_status = resp.status_code
        if resp.status_code == 429:
            saw_429 = True
            break
    check("auth_rate_limit_enforced", saw_429, f"last_status={last_status} attempts={attempts}")

    return r
