#!/usr/bin/env python3
"""Latency benchmark for the laya decision service.

Sends N POST /predict calls with a fixed payload (the checkout-recovery
state from the container smoke), reports wall-time p50/p95/mean/min/max in
milliseconds after one warmup call. Stdlib only.

Usage:
    python3 scripts/laya_bench.py http://127.0.0.1:8090 [N]

The numbers behind docs/latency tables in this repo were produced with
this script; rerun it against any deployment to compare.
"""
import json
import statistics
import sys
import time
import urllib.request

PAYLOAD = json.dumps({
    "state": {
        "url": "https://shop.example.test/checkout",
        "title": "Checkout - Example Shop",
        "visible_text": (
            "Order summary 2 items total $84.00 "
            "Payment method [Payment failed - card declined] "
            "Retry payment  Change card  Contact support"
        ),
    },
    "questions": {
        "next_action": {
            "type": "choice",
            "instructions": "Pick the next UI automation action for a checkout recovery test.",
            "criteria": {
                "retry_payment": "A retry payment control is visible and the failure is transient.",
                "change_card": "The failure suggests the card itself; switch payment method.",
                "escalate": "No recovery path is visible; hand the run to a human.",
                "assert_failure": "The test should record this state as the expected failure.",
            },
        },
        "error_visible": {
            "type": "noul",
            "instructions": "Does the page state show a payment error to the user?",
        },
    },
}).encode()


def one_call(url):
    req = urllib.request.Request(url, data=PAYLOAD,
                                 headers={"Content-Type": "application/json"})
    started = time.perf_counter()
    with urllib.request.urlopen(req, timeout=300) as resp:
        body = json.load(resp)
    ms = (time.perf_counter() - started) * 1000.0
    return ms, "answers" in body and bool(body["answers"])


def pct(sorted_ms, p):
    idx = min(len(sorted_ms) - 1, max(0, round(p / 100.0 * len(sorted_ms)) - 1))
    return sorted_ms[idx]


def main():
    if len(sys.argv) < 2:
        print(__doc__)
        sys.exit(2)
    url = sys.argv[1].rstrip("/") + "/predict"
    n = int(sys.argv[2]) if len(sys.argv) > 2 else 60

    one_call(url)  # warmup: model lazy-init, CUDA context
    samples, bad = [], 0
    for _ in range(n):
        ms, ok = one_call(url)
        if not ok:
            bad += 1
        samples.append(ms)
    samples.sort()
    print(json.dumps({
        "url": url, "n": n, "failures": bad,
        "p50_ms": round(pct(samples, 50), 1),
        "p95_ms": round(pct(samples, 95), 1),
        "mean_ms": round(statistics.fmean(samples), 1),
        "min_ms": round(samples[0], 1), "max_ms": round(samples[-1], 1),
    }))


if __name__ == "__main__":
    main()
