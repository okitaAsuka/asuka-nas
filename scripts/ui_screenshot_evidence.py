#!/usr/bin/env python3
"""Bounded fixture-only screenshot evidence over Actions logs, alongside artifacts.
Useful when the review host cannot materialize an artifact download. No user data.
"""
import base64
import hashlib
from pathlib import Path

# Only screenshots from the production-API test's temporary SQLite/fake services.
for name in (
    "asuka-workflow-real-overview-desktop.png",
    "asuka-workflow-real-overview-mobile.png",
    "asuka-workflow-real-tasks-desktop.png",
    "asuka-workflow-real-tasks-mobile.png",
):
    path = Path("/tmp") / name
    if not path.is_file():
        continue
    data = path.read_bytes()
    if len(data) > 512 * 1024:
        raise RuntimeError("fixture screenshot exceeds evidence limit")
    encoded = base64.b64encode(data).decode("ascii")
    chunks = [encoded[i:i + 4096] for i in range(0, len(encoded), 4096)]
    print("NAS_SCREENSHOT_META", name, hashlib.sha256(data).hexdigest(), len(data), len(chunks))
    for index, chunk in enumerate(chunks):
        print("NAS_SCREENSHOT_DATA", name, index, chunk)
