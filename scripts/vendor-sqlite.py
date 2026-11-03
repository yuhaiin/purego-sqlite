#!/usr/bin/env python3
"""Vendor or verify the official source used by the default CGO static engine."""
import argparse
import hashlib
import importlib.util
import io
import json
import re
from pathlib import Path
import urllib.request
import zipfile

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('native_build', ROOT/'scripts/build-native.py')
build = importlib.util.module_from_spec(spec)
spec.loader.exec_module(build)
TARGET = ROOT/'internal/sqlite3'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--update', action='store_true')
    args = parser.parse_args()
    if args.update:
        with urllib.request.urlopen(build.URL, timeout=60) as response:
            data = response.read()
        if hashlib.sha256(data).hexdigest() != build.SHA256 or hashlib.sha3_256(data).hexdigest() != build.SHA3_256:
            raise ValueError('official SQLite source checksum mismatch')
        manifest = {'version': build.VERSION, 'source_url': build.URL, 'source_sha256': build.SHA256,
                    'source_sha3_256': build.SHA3_256, 'files': {}}
        with zipfile.ZipFile(io.BytesIO(data)) as archive:
            for name in ('sqlite3.c', 'sqlite3.h', 'sqlite3ext.h'):
                content = archive.read(Path(build.ARCHIVE).stem+'/'+name)
                (TARGET/name).write_bytes(content)
                manifest['files'][name] = hashlib.sha256(content).hexdigest()
        (TARGET/'source.json').write_text(json.dumps(manifest, indent=2)+'\n')
        native = TARGET/'native.go'
        if native.exists():
            native.write_text(re.sub(r'const Version = "[^"]+"', 'const Version = "'+build.VERSION+'"', native.read_text()))
    manifest = json.loads((TARGET/'source.json').read_text())
    if manifest['version'] != build.VERSION or manifest['source_url'] != build.URL or manifest['source_sha256'] != build.SHA256 or manifest['source_sha3_256'] != build.SHA3_256:
        raise ValueError('vendored source pin does not match native build pin')
    for name, digest in manifest['files'].items():
        if hashlib.sha256((TARGET/name).read_bytes()).hexdigest() != digest:
            raise ValueError('vendored source integrity failure: '+name)
    print('verified official SQLite '+manifest['version'])


if __name__ == '__main__':
    main()
