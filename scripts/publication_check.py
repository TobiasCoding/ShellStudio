#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-3.0-only
"""Check publishable text, never private runtime directories."""
from pathlib import Path
import re
import sys

ROOT = Path(__file__).resolve().parents[1]
SKIP = {".git", ".work", "bin", "dist", "__pycache__"}
PATTERNS = {
    "absolute home path": r"/home/[a-zA-Z0-9_-]+/",
    "private key": r"-----BEGIN (?:RSA |OPENSSH |EC )?PRIVATE KEY-----",
    "API token": r"(?:sk-ant-|sk-proj-)[A-Za-z0-9_-]{20,}",
    "source workspace leakage": r"/opt/(?:binance-trading|mcp)/",
}
errors = []
count = 0
for path in ROOT.rglob("*"):
    if not path.is_file() or any(p in SKIP for p in path.relative_to(ROOT).parts):
        continue
    count += 1
    if path.name == "logo.png" and path.read_bytes().startswith(b"\x89PNG\r\n\x1a\n"):
        continue
    if path.suffix in {".db", ".sqlite3", ".log", ".pem"}:
        errors.append(f"{path.relative_to(ROOT)}: private file type")
    try:
        text = path.read_text()
    except UnicodeError:
        errors.append(f"{path.relative_to(ROOT)}: unexpected binary")
        continue
    for label, pattern in PATTERNS.items():
        if re.search(pattern, text):
            errors.append(f"{path.relative_to(ROOT)}: {label}")
print(f"Publication scan: {count} files, {len(errors)} findings")
for error in errors:
    print(error)
sys.exit(bool(errors))
