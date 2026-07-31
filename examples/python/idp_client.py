"""
Example Python client for IDP: fetch every config/secret for a
service+environment (decrypted into a dict), and every feature flag
(into a dict of name -> enabled). Both use the same service client API key.

    pip install requests cryptography
    IDP_API_KEY=... IDP_ENCRYPTION_KEY=... python idp_client.py
"""
from __future__ import annotations

import os
from typing import Dict

import requests
from cryptography.fernet import Fernet


def get_decrypted_configs(
    base_url: str,
    api_key: str,
    encryption_key: str,
    service: str,
    environment: str,
) -> Dict[str, str]:
    """Fetch every config/secret for a service+environment and decrypt it into a dict."""
    response = requests.get(
        f"{base_url}/api/v1/config/configs/list",
        params={"service": service, "environment": environment},
        headers={"X-API-Key": api_key},
        timeout=10,
    )
    response.raise_for_status()

    fernet = Fernet(encryption_key.encode())
    return {
        entry["key"]: fernet.decrypt(entry["value"].encode()).decode()
        for entry in response.json()
    }


def get_feature_flags(
    base_url: str,
    api_key: str,
    service: str,
    environment: str,
) -> Dict[str, bool]:
    """Fetch every feature flag for a service+environment into a dict of name -> enabled."""
    response = requests.get(
        f"{base_url}/api/v1/config/feature-flags",
        params={"service": service, "environment": environment},
        headers={"X-API-Key": api_key},
        timeout=10,
    )
    response.raise_for_status()

    return {flag["name"]: flag["is_enabled"] for flag in response.json()}


if __name__ == "__main__":
    base_url = os.environ.get("IDP_BASE_URL", "http://localhost:8000")
    api_key = os.environ["IDP_API_KEY"]

    configs = get_decrypted_configs(
        base_url=base_url,
        api_key=api_key,
        encryption_key=os.environ["IDP_ENCRYPTION_KEY"],
        service="my-app",
        environment="prod",
    )
    print(configs.get("DB_PASSWORD"))

    flags = get_feature_flags(
        base_url=base_url,
        api_key=api_key,
        service="my-app",
        environment="prod",
    )
    if flags.get("NEW_CHECKOUT"):
        print("NEW_CHECKOUT is enabled")
