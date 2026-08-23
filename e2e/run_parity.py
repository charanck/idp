"""
Run the e2e scenario against one or two servers.

    # E2E test against a single server (exits non-zero if any check fails)
    python e2e/run_parity.py --base-url http://localhost:8000

    # Parity check: run the identical scenario against both servers and
    # diff the results (exits non-zero on any failure or mismatch)
    python e2e/run_parity.py \\
        --base-url http://localhost:8000 --label django \\
        --base-url http://localhost:8001 --label go
"""
from __future__ import annotations

import argparse
import sys

from scenario import ScenarioConfig, run


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--base-url", action="append", required=True, help="repeatable: one or two server URLs")
    parser.add_argument("--label", action="append", default=[], help="repeatable: name for each --base-url, in order")
    parser.add_argument("--admin-email", default="admin@control-plane.local")
    parser.add_argument("--admin-password", default="admin123!")
    parser.add_argument("--auth-rate-limit", type=int, default=10, help="must match AUTH_RATE_LIMIT on the server(s)")
    args = parser.parse_args()

    base_urls = args.base_url
    labels = args.label + [f"server{i+1}" for i in range(len(args.label), len(base_urls))]
    if len(base_urls) > 2:
        parser.error("at most two --base-url values are supported")

    results = {}
    for url, label in zip(base_urls, labels):
        print(f"\n=== Running scenario against {label} ({url}) ===")
        cfg = ScenarioConfig(
            base_url=url,
            admin_email=args.admin_email,
            admin_password=args.admin_password,
            auth_rate_limit=args.auth_rate_limit,
        )
        result = run(cfg)
        results[label] = result
        for c in result.checks:
            mark = "PASS" if c.ok else "FAIL"
            timing = f" ({c.elapsed_ms:.1f}ms)" if c.elapsed_ms is not None else ""
            print(f"  [{mark}] {c.name}{timing}" + (f" -- {c.detail}" if not c.ok and c.detail else ""))
        total = len(result.checks)
        passed = sum(1 for c in result.checks if c.ok)
        print(f"  {passed}/{total} checks passed")

    exit_code = 0
    for label, result in results.items():
        if not result.all_ok:
            exit_code = 1

    if len(results) == 2:
        (label_a, result_a), (label_b, result_b) = results.items()
        dict_a, dict_b = result_a.as_dict(), result_b.as_dict()
        all_names = list(dict_a.keys())
        for name in dict_b:
            if name not in dict_a:
                all_names.append(name)

        mismatches = []
        for name in all_names:
            in_a = name in dict_a
            in_b = name in dict_b
            if not in_a or not in_b:
                mismatches.append((name, "ran" if in_a else "missing", "ran" if in_b else "missing"))
                continue
            ok_a, _ = dict_a[name]
            ok_b, _ = dict_b[name]
            if ok_a != ok_b:
                mismatches.append((name, "pass" if ok_a else "fail", "pass" if ok_b else "fail"))

        print(f"\n=== Parity: {label_a} vs {label_b} ===")
        if not mismatches:
            print(f"  All {len(all_names)} checks agree between {label_a} and {label_b}.")
        else:
            exit_code = 1
            print(f"  {len(mismatches)} mismatch(es):")
            for name, a, b in mismatches:
                print(f"    {name}: {label_a}={a} {label_b}={b}")

    return exit_code


if __name__ == "__main__":
    sys.exit(main())
