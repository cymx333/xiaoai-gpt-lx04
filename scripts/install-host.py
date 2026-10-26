#!/usr/bin/env python3
"""Install the Mac companion locally; enable only after Xiaomi login succeeds."""
import argparse
import json
import os
import pathlib
import plistlib
import shutil
import subprocess
import sys


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--serial", required=True)
    parser.add_argument("--address", required=True)
    parser.add_argument("--enable", action="store_true")
    args = parser.parse_args()
    label = "local.xiaoai-gpt.host"
    domain = f"gui/{os.getuid()}"
    if subprocess.run(["launchctl", "print", domain + "/" + label],
                      stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0:
        raise SystemExit("Service is already loaded; stop it explicitly before updating")
    source = pathlib.Path(__file__).resolve().parents[1]
    runtime = pathlib.Path.home() / "Library/Application Support/XiaoAI-GPT"
    runtime.mkdir(parents=True, mode=0o700, exist_ok=True)
    runtime.chmod(0o700)
    for name in (".secrets", "logs"):
        directory = runtime / name
        directory.mkdir(mode=0o700, exist_ok=True)
        directory.chmod(0o700)
    for name in ("lx04-host", "lx04-bootstrap-mac"):
        target = runtime / name
        temporary = runtime / (name + ".new")
        shutil.copy2(source / "dist" / name, temporary)
        temporary.chmod(0o700)
        temporary.replace(target)
    target = runtime / "codex_voice_worker.py"
    shutil.copy2(source / "scripts/codex_voice_worker.py", target)
    target.chmod(0o600)
    codex = shutil.which("codex")
    if not codex:
        raise SystemExit("Codex executable not found")
    config_path = runtime / ".secrets/speaker-config.json"
    program = [str(runtime / "lx04-host"), "-config", str(config_path),
               "-serial", args.serial, "-address", args.address,
               "-python", sys.executable, "-worker", str(target)]
    plist_path = pathlib.Path.home() / "Library/LaunchAgents" / (label + ".plist")
    staged_plist = runtime / "launch-agent.plist"
    payload = {"Label": label, "ProgramArguments": program,
               "WorkingDirectory": str(runtime),
               "EnvironmentVariables": {"XIAOAI_CODEX": codex, "PATH": "/usr/bin:/bin:/usr/sbin:/sbin"},
               "RunAtLoad": True, "KeepAlive": True, "ThrottleInterval": 30,
               "ProcessType": "Background",
               "StandardOutPath": str(runtime / "logs/host.log"),
               "StandardErrorPath": str(runtime / "logs/host.log")}
    with staged_plist.open("wb") as file:
        plistlib.dump(payload, file)
    staged_plist.chmod(0o600)
    manifest = {"runtime": str(runtime), "address": args.address,
                "serial": args.serial, "model": "gpt-6-luna", "launch_agent": str(plist_path)}
    (runtime / "installation.json").write_text(json.dumps(manifest, indent=2) + "\n")
    if args.enable:
        config = json.loads(config_path.read_text())
        state = json.loads((runtime / ".secrets/bootstrap-status.json").read_text())
        if state.get("status") != "ready" or not config.get("mi_did"):
            raise SystemExit("Xiaomi bootstrap must complete before activation")
        plist_path.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(staged_plist, plist_path)
        subprocess.run(["launchctl", "bootstrap", domain, str(plist_path)], check=True)
    print(json.dumps({"installed": True, "enabled": args.enable, "runtime": str(runtime)}, ensure_ascii=False))


if __name__ == "__main__":
    main()
