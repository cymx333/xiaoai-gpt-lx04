#!/usr/bin/env python3
# Copyright 2026 cymx333 and contributors.
# SPDX-License-Identifier: Apache-2.0
"""Install to one explicitly matched LX04. Never prints credential values.
Run --dry-run first; requires existing root ADB. Does not flash or obtain root.
"""
import argparse
import json
from pathlib import Path
import re
import shlex
import subprocess
import uuid

PACKAGE = 'local.xiaoaigpt'
APP = '/data/user/0/' + PACKAGE + '/files'


def normalize(value):
    return re.sub(r'[:/\-\s]', '', str(value)).upper()


def matches(selector, serial, mac):
    expected = [normalize(selector.get(k, '')) for k in ('serial_number', 'mac')]
    return any(expected) and all(not e or e == normalize(v) for e, v in zip(expected, (serial, mac)))


def validate_credentials(folder):
    config = json.loads((folder / 'speaker-config.json').read_text())
    account = json.loads((folder / 'chatgpt-account.json').read_text())
    if config.get('openai_key'):
        raise ValueError('GPT OAuth mode does not need an API Key; clear openai_key in the private config.')
    if config.get('hardware') != 'LX04' or not all(config.get(k) for k in ('account', 'password', 'mi_did')):
        raise ValueError('Complete the Xiaomi bootstrap first.')
    required = ('client_id', 'subject', 'access_token', 'refresh_token', 'ext_agent_host_id')
    if (account.get('issuer') != 'https://auth.openai.com' or
            not all(account.get(k) for k in required) or
            not str(account['client_id']).startswith('oaiapp_') or
            'chatgpt.tokens.use.direct' not in account.get('scopes', [])):
        raise ValueError('Complete the independent ChatGPT authorization first.')
    if not (folder / '.mi.token').is_file() or not (folder / '.mi.token').stat().st_size:
        raise ValueError('Missing Xiaomi session file.')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--adb', default='adb')
    parser.add_argument('--device', required=True, help='exact adb -s transport identifier')
    parser.add_argument('--device-file', required=True, type=Path)
    parser.add_argument('--apk', type=Path, required=True)
    parser.add_argument('--private-dir', type=Path, help='contains speaker-config.json, .mi.token and chatgpt-account.json')
    parser.add_argument('--dry-run', action='store_true', help='read-only validation; no install or changes')
    parser.add_argument('--update-only', action='store_true', help='replace APK, preserve device credentials')
    parser.add_argument('--allow-unauthenticated-network', action='store_true', help='explicitly accept unencrypted, unauthenticated LAN ADB on a trusted isolated network')
    args = parser.parse_args()
    prefix = [args.adb, '-s', args.device]

    def adb(*parts, check=True, timeout=90):
        result = subprocess.run(prefix + list(parts), capture_output=True, text=True, timeout=timeout)
        if check and result.returncode:
            raise RuntimeError('ADB operation failed; output suppressed to protect account data.')
        return result.stdout.strip()

    def shell(command, **kwargs):
        return adb('shell', command, **kwargs)

    if not args.apk.is_file():
        raise ValueError('APK does not exist; build it first.')
    selector = json.loads(args.device_file.read_text())
    if shell('id -u') != '0':
        raise ValueError('Existing root ADB is required for first-time permissions and private configuration.')
    if shell('getprop ro.product.model') != 'LX04' or shell('getprop ro.build.version.sdk') != '27':
        raise ValueError('This installer only supports the validated LX04 / Android 8.1 combination.')
    if not matches(selector, shell('getprop ro.serialno'), shell('cat /sys/class/net/wlan0/address')):
        raise ValueError('Exact speaker identity mismatch; no changes made.')
    network = ':' in args.device
    if network and shell('getprop ro.adb.secure') != '1' and not args.allow_unauthenticated_network:
        raise ValueError('Unauthenticated network ADB refused. Prefer USB or authenticated ADB; read the explicit opt-in flag before overriding.')
    if network:
        print('Network ADB transport: use a trusted isolated network; ADB does not encrypt this transfer.')
    exists = shell('test -f ' + shlex.quote(APP + '/chatgpt-account.json') + ' && echo yes || echo no') == 'yes'
    if args.update_only:
        if not exists:
            raise ValueError('No existing configured app; use first-time installation.')
    else:
        if exists:
            raise ValueError('A configured account already exists; use --update-only to avoid overwriting it.')
        if args.private_dir is None:
            raise ValueError('--private-dir is required for first-time installation.')
        validate_credentials(args.private_dir)
    print('Device identity, prerequisites and local files validated; credential values suppressed.')
    if args.dry_run:
        print('Dry run complete; no changes made.')
        return
    stage = '/data/local/tmp/xiaoai-gpt-setup-' + uuid.uuid4().hex
    shell('umask 077; mkdir ' + stage + '; chmod 700 ' + stage)
    try:
        # Stop old companion/standalone services yourself before first installation.
        adb('install', '-r', str(args.apk.resolve()), timeout=180)
        for permission in ('READ_LOGS', 'WRITE_SECURE_SETTINGS'):
            shell('pm grant ' + PACKAGE + ' android.permission.' + permission)
        info = shell('dumpsys package ' + PACKAGE)
        found = re.search(r'\buserId=(\d+)\b', info)
        if not found or int(found.group(1)) < 10000:
            raise RuntimeError('Cannot determine installed application UID.')
        uid = found.group(1)
        if not args.update_only:
            names = ('speaker-config.json', '.mi.token', 'chatgpt-account.json')
            shell('mkdir -p ' + APP + '; chown ' + uid + ':' + uid + ' ' + APP + '; chmod 700 ' + APP)
            for name in names:
                adb('push', str((args.private_dir / name).resolve()), stage + '/' + name)
                shell('set -e; cp ' + stage + '/' + name + ' ' + APP + '/' + name +
                      '; chown ' + uid + ':' + uid + ' ' + APP + '/' + name +
                      '; chmod 600 ' + APP + '/' + name)
            shell('restorecon -R ' + APP)
        shell('dumpsys deviceidle whitelist +' + PACKAGE)
        shell('am start -n ' + PACKAGE + '/.MainActivity')
        shell('am start-foreground-service -n ' + PACKAGE + '/.VoiceService -a local.xiaoaigpt.MAINTENANCE')
        print('Installed. Maintenance stays open for about ten minutes. Verify voice and power-cycle behavior, then close maintenance.')
    finally:
        try:
            shell('rm -f ' + stage + '/speaker-config.json ' + stage + '/.mi.token ' + stage +
                  '/chatgpt-account.json; rmdir ' + stage, timeout=10)
        except (RuntimeError, subprocess.TimeoutExpired):
            print('Temporary staging cleanup could not be verified; inspect the task-owned xiaoai-gpt-setup directory during maintenance.')


if __name__ == '__main__':
    try:
        main()
    except json.JSONDecodeError:
        import sys
        print('A local JSON file is malformed; values suppressed.', file=sys.stderr)
        raise SystemExit(1)
    except (ValueError, RuntimeError) as error:
        import sys
        print(str(error), file=sys.stderr)
        raise SystemExit(1)
    except (OSError, subprocess.TimeoutExpired):
        # Avoid library exceptions accidentally echoing credentials/JSON payloads.
        import sys
        print('Installation stopped. Check prerequisites, device selector, private files, and maintenance access. No credential values are printed.', file=sys.stderr)
        raise SystemExit(1)
