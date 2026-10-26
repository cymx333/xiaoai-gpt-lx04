#!/usr/bin/env python3
"""One bounded voice request per line; official Codex managed ChatGPT login.

No credentials are copied. No shell, files, apps, MCP servers or agent tools are
available to the voice model. stdout contains only our own response envelopes.
"""
import json
import os
import pathlib
import queue
import re
import subprocess
import sys
import threading
import time
import tomllib
from datetime import datetime
from zoneinfo import ZoneInfo


class CodexVoice:
    def __init__(self):
        self.queue = queue.Queue()
        self.sequence = 0
        self.pending = []
        self.config = {
            "features.apps": False, "features.shell_tool": False,
            "features.multi_agent": False, "features.hooks": False,
            "features.remote_plugin": False, "features.memories": False,
            "features.skill_search": False, "features.skip_host_skill_discovery": True,
            "web_search": "disabled", "project_doc_max_bytes": 0,
        }
        config_path = pathlib.Path.home() / ".codex/config.toml"
        if config_path.exists():
            config = tomllib.loads(config_path.read_text())
            for name in config.get("mcp_servers", {}):
                if not re.fullmatch(r"[A-Za-z0-9_-]+", name):
                    raise RuntimeError("Unsupported MCP configuration name")
                self.config[f'mcp_servers.{name}.enabled'] = False
        args = [os.environ.get("XIAOAI_CODEX", "codex"), "app-server"]
        for key, value in self.config.items():
            args.extend(["-c", key + "=" + json.dumps(value)])
        self.process = subprocess.Popen(args, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                        stderr=subprocess.DEVNULL, text=True)
        threading.Thread(target=self._read, daemon=True).start()
        try:
            self.rpc("initialize", {"clientInfo": {"name": "lx04_voice_bridge", "version": "0.1.0"},
                                    "capabilities": {"experimentalApi": True}}, time.monotonic() + 20)
            self.send({"method": "initialized", "params": {}})
            account = self.rpc("account/read", {"refreshToken": False}, time.monotonic() + 20)
            if (account.get("account") or {}).get("type") != "chatgpt":
                raise RuntimeError("ChatGPT login is required")
        except Exception:
            self.close()
            raise

    def _read(self):
        for line in self.process.stdout:
            try:
                self.queue.put(json.loads(line))
            except ValueError:
                pass
        self.queue.put({"worker_eof": True})

    def send(self, message):
        self.process.stdin.write(json.dumps(message) + "\n")
        self.process.stdin.flush()

    def receive(self, deadline):
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise TimeoutError()
        event = self.queue.get(timeout=remaining)
        if event.get("worker_eof"):
            raise RuntimeError("Codex disconnected")
        if "id" in event and "method" in event:
            self.send({"id": event["id"], "error": {"code": -32601, "message": "Tools disabled"}})
        return event

    def rpc(self, method, params, deadline):
        self.sequence += 1
        request_id = self.sequence
        self.send({"id": request_id, "method": method, "params": params})
        while True:
            event = self.receive(deadline)
            if event.get("id") == request_id:
                if "error" in event:
                    raise RuntimeError("Codex request rejected")
                return event.get("result", {})
            self.pending.append(event)

    def answer(self, text):
        if not isinstance(text, str) or not text.strip() or len(text) > 2000:
            raise ValueError("Invalid voice query")
        self.pending.clear()
        deadline = time.monotonic() + 20
        today = datetime.now(ZoneInfo("Asia/Shanghai")).strftime("%Y年%m月%d日")
        result = self.rpc("thread/start", {
            "model": "gpt-6-luna", "allowProviderModelFallback": False,
            "ephemeral": True, "environments": [], "cwd": os.getcwd(),
            "approvalPolicy": "never", "sandbox": "read-only", "config": self.config,
            "baseInstructions": "你是小爱音箱的中文语音问答助手。今天是" + today +
            "。只回答用户的问题，不调用工具，不执行设备操作。用自然中文直接回答，通常不超过100字，不使用Markdown。"
            "你没有联网搜索工具，遇到无法核实的时效信息要明确说明，不能编造。",
        }, deadline)
        thread_id = result["thread"]["id"]
        if result.get("model") != "gpt-6-luna":
            raise RuntimeError("Unexpected model")
        started = self.rpc("turn/start", {"threadId": thread_id, "model": "gpt-6-luna",
            "effort": "none", "environments": [], "input": [{"type": "text", "text": text}]}, deadline)
        turn_id = started["turn"]["id"]
        parts = []
        try:
            while True:
                event = self.pending.pop(0) if self.pending else self.receive(deadline)
                params = event.get("params", {})
                if params.get("threadId") != thread_id:
                    continue
                if event.get("method") == "item/agentMessage/delta":
                    parts.append(params.get("delta", ""))
                if event.get("method") == "turn/completed":
                    if params.get("turn", {}).get("status") != "completed":
                        raise RuntimeError("Inference failed")
                    answer = "".join(parts).strip()
                    if not answer:
                        raise RuntimeError("Empty inference result")
                    return answer
        finally:
            self.send({"id": self.sequence + 1000000, "method": "turn/interrupt",
                       "params": {"threadId": thread_id, "turnId": turn_id}})
            self.send({"id": self.sequence + 2000000, "method": "thread/unsubscribe",
                       "params": {"threadId": thread_id}})

    def close(self):
        if hasattr(self, "process") and self.process.poll() is None:
            self.process.terminate()
            try:
                self.process.wait(timeout=3)
            except subprocess.TimeoutExpired:
                self.process.kill()
                self.process.wait()


def main():
    client = None
    try:
        client = CodexVoice()
        print(json.dumps({"ready": True, "model": "gpt-6-luna"}), flush=True)
        for line in sys.stdin:
            started = time.monotonic()
            try:
                query = json.loads(line)
                answer = client.answer(query["text"])
                print(json.dumps({"text": answer, "elapsed_seconds": round(time.monotonic()-started, 3)}, ensure_ascii=False), flush=True)
            except Exception:
                print(json.dumps({"error": "model_request_failed"}), flush=True)
                break
    except Exception:
        print(json.dumps({"error": "worker_initialization_failed"}), flush=True)
    finally:
        if client:
            client.close()


if __name__ == "__main__":
    main()
