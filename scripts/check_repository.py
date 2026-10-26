#!/usr/bin/env python3
"""Check publishable files without importing hardware scripts or printing secrets."""
# SPDX-License-Identifier: Apache-2.0
import ast
import json
from pathlib import Path
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
PRIVATE_DIRS = {'.secrets', 'backups', 'artifacts', 'logs', 'tools', 'dist', '.venv'}
PRIVATE_NAMES = {'.env', '.mi.token', 'speaker-config.json', 'device-match.json',
                 'adbkey', 'adbkey.pub', 'otp.txt', 'otp-session.json', 'config.json',
                 '小米账号.json', '小米验证码.txt', 'dns-server.txt', 'last-search.json',
                 'chatgpt-account.json', 'host.json', 'signin-page.json', 'status.json',
                 'deployment-preflight.json', 'installation.json'}
PATTERNS = [re.compile(rb'-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----'),
            re.compile(rb'\b(?:gh[pousr]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{30,})\b'),
            re.compile(rb'\bsk-[A-Za-z0-9_-]{24,}\b'),
            re.compile(rb'\beyJ[A-Za-z0-9_-]{16,}\.[A-Za-z0-9_-]{16,}\.[A-Za-z0-9_-]{16,}\b')]


def main():
    raw = subprocess.check_output(['git', '-c', 'core.quotepath=false', 'ls-files',
                                   '--cached', '--others', '--exclude-standard', '-z'], cwd=ROOT)
    files = sorted(set(p for p in raw.decode().split('\0') if p))
    failures = []
    for relative in files:
        path = ROOT / relative
        if path.is_symlink():
            failures.append(f'{relative}: symlink requires manual review')
            continue
        if not path.is_file():
            failures.append(f'{relative}: missing file')
            continue
        if path.parts and (Path(relative).parts[0] in PRIVATE_DIRS or path.name in PRIVATE_NAMES):
            failures.append(f'{relative}: private path must not be published')
            continue
        if path.suffix.lower() in {'.p12', '.pfx', '.jks', '.keystore', '.password', '.pem', '.key', '.apk'}:
            failures.append(f'{relative}: credential or binary artifact must not be published')
            continue
        data = path.read_bytes()
        if any(pattern.search(data) for pattern in PATTERNS):
            failures.append(f'{relative}: possible credential; value suppressed')
        try:
            if path.suffix == '.json':
                parsed = json.loads(data)
                def inspect(value):
                    if isinstance(value, dict):
                        for key, item in value.items():
                            if key.lower() in {'password', 'access_token', 'refresh_token', 'id_token', 'client_secret', 'openai_key', 'cookie', 'ssecurity', 'service_token'} and isinstance(item, str) and item:
                                failures.append(f'{relative}: nonempty credential field; value suppressed')
                            inspect(item)
                    elif isinstance(value, list):
                        for item in value: inspect(item)
                inspect(parsed)
            elif path.suffix == '.py':
                ast.parse(data, filename=relative)
        except (ValueError, SyntaxError) as error:
            failures.append(f'{relative}: invalid syntax ({type(error).__name__})')
    if failures:
        print('\n'.join(failures), file=sys.stderr)
        return 1
    print(f'Repository audit passed: {len(files)} source/document files; no hardware execution.')
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
