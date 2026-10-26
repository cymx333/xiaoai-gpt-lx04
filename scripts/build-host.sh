#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
mkdir -p "$ROOT/dist"
cd "$ROOT/client"
go test ./hostlink ./cmd/lx04-host ./devicevoice
CGO_ENABLED=0 go build -trimpath -o "$ROOT/dist/lx04-host" ./cmd/lx04-host
CGO_ENABLED=0 go build -trimpath -o "$ROOT/dist/lx04-bootstrap-mac" ./cmd/lx04-bootstrap
python3 -m py_compile "$ROOT/scripts/codex_voice_worker.py"
