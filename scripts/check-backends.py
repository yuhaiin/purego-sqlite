#!/usr/bin/env python3
"""Check build-tag selection without requiring cross C toolchains."""
import json
import os
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def check(goos, arch, cgo, forced=False):
    env = dict(os.environ, GOOS=goos, GOARCH=arch, CGO_ENABLED=str(cgo))
    args = ["go", "list", "-json"]
    if forced:
        args += ["-tags", "sqlite_cgo"]
    args += ["./internal/ffi"]
    package = json.loads(subprocess.check_output(args, cwd=ROOT, env=env))
    files = package.get("GoFiles", []) + package.get("CgoFiles", [])
    primary = goos in ("linux", "darwin", "windows") and arch in ("amd64", "arm64")
    expected = "register.go" if primary and not forced else ("register_cgo.go" if cgo else "register_unsupported.go")
    selected = set(files) & {"register.go", "register_cgo.go", "register_unsupported.go"}
    if selected != {expected}:
        raise SystemExit(f"wrong backend for {goos}/{arch} cgo={cgo} forced={forced}: {selected}, want {expected}")
    if expected != "register_cgo.go" and package.get("CgoFiles"):
        raise SystemExit("unexpected production CGO dependency")
    print(f"{goos}/{arch} cgo={cgo} forced={forced}: {expected}")


for target in [("linux", "amd64"), ("linux", "arm64"), ("darwin", "amd64"),
               ("darwin", "arm64"), ("windows", "amd64"), ("windows", "arm64"),
               ("linux", "mipsle"), ("linux", "386"), ("freebsd", "amd64"),
               ("freebsd", "arm64"), ("openbsd", "amd64"), ("netbsd", "amd64"),
               ("dragonfly", "amd64"), ("windows", "386")]:
    for enabled in (0, 1):
        check(*target, enabled)
for enabled in (0, 1):
    check("linux", "amd64", enabled, forced=True)
