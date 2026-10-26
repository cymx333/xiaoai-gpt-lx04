#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
python3 "$ROOT/scripts/check_repository.py"
for file in "$ROOT"/scripts/*.sh; do sh -n "$file"; done
python3 "$ROOT/scripts/test_install_android.py"
python3 "$ROOT/scripts/test_codex_voice_worker.py"
cd "$ROOT/client"
go test . ./devicevoice ./runtimeenv ./jarvis/openai ./miservice ./deepseekweb ./voiceanswer \
    ./cmd/lx04-agent ./cmd/lx04-bootstrap ./cmd/lx04-netcheck ./cmd/lx04-searchcheck \
    ./chatgptdirect ./cmd/lx04-gpt-agent ./cmd/lx04-gptcheck ./hostlink ./cmd/lx04-host
