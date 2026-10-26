#!/usr/bin/env python3
# Copyright 2026 cymx333 and contributors.
# SPDX-License-Identifier: Apache-2.0
"""Build static ARMv7 agent with pidfd optimization disabled for Android 8.
The old app seccomp policy kills pidfd_open rather than returning ENOSYS.
This overlay changes only our build; no device security policy is disabled.
"""
import argparse,json,os,subprocess,tempfile
from pathlib import Path
p=argparse.ArgumentParser();p.add_argument('--go-root',required=True);p.add_argument('--source',required=True);p.add_argument('--output',required=True);a=p.parse_args()
root=Path(a.go_root).resolve();source=Path(a.source).resolve()
Path(a.output).resolve().parent.mkdir(parents=True,exist_ok=True)
original=root/'src/os/pidfd_linux.go'
text=original.read_text()
needle='func checkPidfd() error {\n'
if text.count(needle)!=1:raise SystemExit('Unexpected Go pidfd source; inspect before adapting')
patch=text.replace(needle,needle+'\t// LX04 Android 8 ARMv7: pidfd_open is rejected with SIGSYS by app seccomp.\n\t// Use Go\'s existing waitid/wait4 process lifecycle fallback.\n\tif runtime.GOARCH == "arm" { return NewSyscallError("pidfd_open", syscall.ENOSYS) }\n',1)
with tempfile.TemporaryDirectory(prefix='lx04-go-compat-') as temp:
 temp=Path(temp);replacement=temp/'pidfd_linux.go';replacement.write_text(patch)
 overlay=temp/'overlay.json';overlay.write_text(json.dumps({'Replace':{str(original):str(replacement)}}))
 env={**os.environ,'CGO_ENABLED':'0','GOOS':'linux','GOARCH':'arm','GOARM':'7','GOTOOLCHAIN':'local'}
 subprocess.run([str(root/'bin/go'),'build','-overlay',str(overlay),'-trimpath','-ldflags=-s -w','-o',str(Path(a.output).resolve()),'./cmd/lx04-gpt-agent'],cwd=source,env=env,check=True)
