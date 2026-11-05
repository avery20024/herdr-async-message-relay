#!/usr/bin/env python3
"""以非模型 fixture 驗證原生 exec、重啟世代與 Hook 環境；不啟動服務。"""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parent.parent

class ClaudeRunTest(unittest.TestCase):
    def test_restart_and_late_hooks_use_actual_exec_environment(self):
        with tempfile.TemporaryDirectory(prefix="hrelay-run-") as temp:
            root = Path(temp).resolve()
            binary = root / "hrelay"
            subprocess.run(["go", "build", "-o", str(binary), "./cmd/hrelay"], cwd=ROOT,
                           check=True, capture_output=True, timeout=90)
            harness = root / "fake-claude"
            harness.write_text("#!" + sys.executable + "\n" + '''import json, os, sys
keys = ["DELIVER_ROOT", "DELIVER_STATE_DIR", "HERDR_RELAY_FROM", "HERDR_PANE_ID", "HRELAY_GUARD_BIN", "HRELAY_CLAUDE_RUN_ID"]
print(json.dumps({"args": sys.argv[1:], "context": {k: os.environ.get(k) for k in keys}}))
''')
            harness.chmod(0o700)
            env = dict(os.environ, DELIVER_STATE_DIR=str(root / "state"))
            command = [str(binary), "--root", str(root), "claude-run", "--role", "reviewer",
                       "--pane", "wC:p5", "--", str(harness), "--resume", "same-session"]
            def launch():
                result = subprocess.run(command, env=env, capture_output=True, text=True,
                                        timeout=10, check=True)
                data = json.loads(result.stdout)
                self.assertEqual(data["args"], ["--resume", "same-session"])
                context = data["context"]
                self.assertEqual(context["DELIVER_ROOT"], str(root))
                self.assertEqual(context["HERDR_RELAY_FROM"], "reviewer")
                self.assertEqual(context["HRELAY_GUARD_BIN"], str(binary))
                return dict(env, **context)
            def hook(context, name, tool_id):
                event = {"hook_event_name": name, "session_id": "same-session",
                         "tool_name": "AskUserQuestion", "tool_use_id": tool_id}
                return subprocess.run([str(binary), "--root", str(root), "claude-hook",
                                       "--role", "reviewer", "--pane", "wC:p5"],
                                      input=json.dumps(event), env=context, text=True,
                                      capture_output=True, timeout=10, check=True)
            state_file = root / "state/claude-guard/reviewer/state.json"
            first = launch()
            hook(first, "PreToolUse", "old")  # 不依賴 SessionStart。
            second = launch()
            self.assertNotEqual(first["HRELAY_CLAUDE_RUN_ID"], second["HRELAY_CLAUDE_RUN_ID"])
            hook(second, "PreToolUse", "current")
            state = json.loads(state_file.read_text())
            self.assertEqual(state["pending"], {"current": True})
            self.assertGreater(state["run_started_at"], 0)
            before_stale = state_file.read_text()
            stale = hook(first, "PostToolUse", "current")
            self.assertIn("stale hook harness generation", stale.stderr)
            self.assertEqual(state_file.read_text(), before_stale)
            hook(second, "PostToolUse", "current")
            self.assertEqual(json.loads(state_file.read_text())["pending"], {})
            self.assertFalse((root / "state/deliver.sock").exists())

if __name__ == "__main__":
    unittest.main()
