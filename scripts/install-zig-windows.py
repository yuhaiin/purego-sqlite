#!/usr/bin/env python3
"""Install a checksum-pinned Windows CGO cross compiler (x64 also runs on ARM64)."""
import hashlib
import os
from pathlib import Path
import urllib.request
import zipfile

ROOT = Path(__file__).resolve().parents[1]
URL = 'https://ziglang.org/download/0.17.0/zig-x86_64-windows-0.17.0.zip'
DIGEST = 'b5663f69581dcf391293fbf16c06cb80d81d806545ce618b4d0bab7f0eb8c428'
archive = ROOT/'.native/zig.zip'
archive.parent.mkdir(parents=True, exist_ok=True)
with urllib.request.urlopen(URL, timeout=120) as response:
    archive.write_bytes(response.read())
if hashlib.sha256(archive.read_bytes()).hexdigest() != DIGEST:
    raise SystemExit('Zig compiler SHA-256 mismatch')
with zipfile.ZipFile(archive) as source:
    source.extractall(ROOT/'.native')
path = str(ROOT/'.native/zig-x86_64-windows-0.17.0')
with open(os.environ['GITHUB_PATH'], 'a') as output:
    output.write(path+'\n')
