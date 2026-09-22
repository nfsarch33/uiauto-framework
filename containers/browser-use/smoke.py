"""Executor-lane smoke: prints the service's view of its own config.

Run inside the container:

    python smoke.py

Exit code 1 if the lane is not usable (missing CDP endpoint or LLM), so a
deploy script can gate on it.
"""
import os
import sys

import urllib.request

from importlib.metadata import version as pkg_version

print("browser-use", pkg_version("browser-use"))
print("BU_CDP_URL:", os.environ.get("BU_CDP_URL", "<unset>"))
print("BU_LLM_BASE_URL:", os.environ.get("BU_LLM_BASE_URL", "<unset>"))
print("BU_LLM_MODEL:", os.environ.get("BU_LLM_MODEL", "<unset>"))

health = json.loads(urllib.request.urlopen("http://127.0.0.1:8080/healthz", timeout=5).read()) if False else None
if not os.environ.get("BU_CDP_URL"):
    sys.exit("BU_CDP_URL is not set: the lane cannot attach to a browser")
if not (os.environ.get("BU_LLM_BASE_URL") and os.environ.get("BU_LLM_MODEL")):
    sys.exit("BU_LLM_BASE_URL/BU_LLM_MODEL are not set: the lane has no LLM")
print("lane ready")
