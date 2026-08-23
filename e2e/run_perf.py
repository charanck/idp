"""
Compare request latency/throughput between two running servers for a handful
of representative operations (JWT login, JWT-authenticated write,
API-key-authenticated read - the hot path for service clients polling
configs/flags).

    python e2e/run_perf.py \\
        --base-url http://localhost:8000 --label django \\
        --base-url http://localhost:8001 --label go \\
        --requests 200 --concurrency 10
"""
from __future__ import annotations

import argparse
import statistics
import sys
import time
import uuid
from concurrent.futures import ThreadPoolExecutor
from dataclasses import dataclass

import requests


@dataclass
class Setup:
    admin_headers: dict
    api_headers: dict
    service: str
    environment: str


def bootstrap(base: str, admin_email: str, admin_password: str) -> Setup:
    """Log in as admin and create a service client + one config, so timed
    requests hit realistic, already-populated endpoints rather than 404s."""
    s = requests.Session()
    run_id = uuid.uuid4().hex[:8]

    resp = s.post(f"{base}/api/v1/auth/token", json={"username": admin_email, "password": admin_password})
    resp.raise_for_status()
    admin_headers = {"Authorization": f"Bearer {resp.json()['access_token']}"}

    resp = s.post(f"{base}/api/v1/auth/s2s/clients", json={"name": f"perf-client-{run_id}"}, headers=admin_headers)
    resp.raise_for_status()
    api_headers = {"X-API-Key": resp.json()["api_key"]}

    service, environment = f"perf-svc-{run_id}", "production"
    resp = s.post(f"{base}/api/v1/config/configs/upsert", json={
        "service": service, "environment": environment, "key": "GREETING",
        "value": "hello", "is_secret": False, "type": "string",
    }, headers=admin_headers)
    resp.raise_for_status()

    return Setup(admin_headers, api_headers, service, environment)


def timed_request(fn) -> float:
    start = time.perf_counter()
    resp = fn()
    resp.raise_for_status()
    return (time.perf_counter() - start) * 1000


def run_load(base: str, name: str, fn, n: int, concurrency: int) -> dict:
    latencies = []
    errors = 0

    def task(_):
        nonlocal errors
        try:
            return timed_request(fn)
        except Exception as exc:
            errors += 1
            if errors <= 3:
                print(f"    [debug] error #{errors}: {exc!r}")
            return None

    start = time.perf_counter()
    with ThreadPoolExecutor(max_workers=concurrency) as pool:
        for result in pool.map(task, range(n)):
            if result is not None:
                latencies.append(result)
    wall_s = time.perf_counter() - start

    latencies.sort()
    def pct(p):
        if not latencies:
            return float("nan")
        idx = min(len(latencies) - 1, int(len(latencies) * p))
        return latencies[idx]

    return {
        "name": name,
        "n": n,
        "errors": errors,
        "throughput_rps": n / wall_s if wall_s > 0 else float("nan"),
        "mean_ms": statistics.mean(latencies) if latencies else float("nan"),
        "p50_ms": pct(0.50),
        "p95_ms": pct(0.95),
        "p99_ms": pct(0.99),
        "max_ms": latencies[-1] if latencies else float("nan"),
    }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--base-url", action="append", required=True)
    parser.add_argument("--label", action="append", default=[])
    parser.add_argument("--admin-email", default="admin@control-plane.local")
    parser.add_argument("--admin-password", default="admin123!")
    parser.add_argument("--requests", type=int, default=200)
    parser.add_argument("--concurrency", type=int, default=10)
    args = parser.parse_args()

    base_urls = args.base_url
    labels = args.label + [f"server{i+1}" for i in range(len(args.label), len(base_urls))]

    all_results = {}
    for base, label in zip(base_urls, labels):
        print(f"\n=== Bootstrapping {label} ({base}) ===")
        setup = bootstrap(base, args.admin_email, args.admin_password)
        session = requests.Session()

        scenarios = {
            "POST /auth/token (JWT login)": lambda: session.post(
                f"{base}/api/v1/auth/token", json={"username": args.admin_email, "password": args.admin_password}),
            "GET /auth/me (JWT auth)": lambda: session.get(
                f"{base}/api/v1/auth/me", headers=setup.admin_headers),
            "POST /config/configs/upsert (JWT write)": lambda: session.post(
                f"{base}/api/v1/config/configs/upsert", headers=setup.admin_headers, json={
                    "service": setup.service, "environment": setup.environment, "key": "GREETING",
                    "value": f"hello-{uuid.uuid4().hex[:6]}", "is_secret": False, "type": "string",
                }),
            "GET /config/configs/list (API-key read)": lambda: session.get(
                f"{base}/api/v1/config/configs/list", headers=setup.api_headers,
                params={"service": setup.service, "environment": setup.environment}),
        }

        label_results = []
        for name, fn in scenarios.items():
            res = run_load(base, name, fn, args.requests, args.concurrency)
            label_results.append(res)
            print(f"  {name}")
            print(f"    throughput={res['throughput_rps']:.1f} req/s  "
                  f"mean={res['mean_ms']:.2f}ms  p50={res['p50_ms']:.2f}ms  "
                  f"p95={res['p95_ms']:.2f}ms  p99={res['p99_ms']:.2f}ms  "
                  f"errors={res['errors']}/{res['n']}")
        all_results[label] = label_results

    if len(all_results) == 2:
        (label_a, results_a), (label_b, results_b) = all_results.items()
        print(f"\n=== {label_a} vs {label_b} (p50 / throughput) ===")
        by_name_b = {r["name"]: r for r in results_b}
        for ra in results_a:
            rb = by_name_b.get(ra["name"])
            if rb is None:
                continue
            speedup = ra["p50_ms"] / rb["p50_ms"] if rb["p50_ms"] else float("nan")
            faster = label_b if speedup > 1 else label_a
            print(f"  {ra['name']}")
            print(f"    {label_a}: p50={ra['p50_ms']:.2f}ms throughput={ra['throughput_rps']:.1f} req/s")
            print(f"    {label_b}: p50={rb['p50_ms']:.2f}ms throughput={rb['throughput_rps']:.1f} req/s")
            print(f"    -> {faster} is {max(speedup, 1/speedup):.2f}x faster (p50)")

    return 0


if __name__ == "__main__":
    sys.exit(main())
