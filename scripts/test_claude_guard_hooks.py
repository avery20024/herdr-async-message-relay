import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest


class ClaudeGuardHookExampleTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="hrelay-hook-example-")
        self.addCleanup(self.temp.cleanup)
        self.base = Path(self.temp.name)
        self.binary = self.base / "fake hrelay"
        self.binary.write_text(
            '#!/bin/sh\n'
            'printf "%s\\n" "$@" > "$CAPTURE_ARGS"\n'
            '/bin/cat > "$CAPTURE_STDIN"\n'
            'exit "${FAKE_EXIT:-0}"\n'
        )
        self.binary.chmod(0o700)
        self.env = {
            "PATH": "/nonexistent",
            "HOME": str(self.base),
            "HRELAY_GUARD_BIN": str(self.binary),
            "DELIVER_ROOT": str(self.base / "team with 'quotes' ; $literal"),
            "HERDR_RELAY_FROM": "gm",
            "HERDR_PANE_ID": "wC:p5",
            "CAPTURE_ARGS": str(self.base / "args"),
            "CAPTURE_STDIN": str(self.base / "stdin"),
        }
        path = Path(__file__).resolve().parents[1] / "examples/claude-question-guard.settings.json"
        self.settings = json.loads(path.read_text())["hooks"]
        self.shells = [path for path in ["/bin/sh", "/bin/bash"] if os.path.isfile(path)]

    def run_hook(self, shell, event, env):
        hook = self.settings[event][0]["hooks"][0]
        payload = json.dumps({"hook_event_name": event, "session_id": "s1", "tool_name": "AskUserQuestion", "tool_use_id": "q1"}) + "\n"
        result = subprocess.run(
            [shell, "-c", hook["command"]], env=env, input=payload,
            text=True, capture_output=True, timeout=5,
        )
        return result, payload

    def test_example_is_synchronous_and_question_scoped(self):
        self.assertEqual(set(self.settings), {"SessionStart", "SessionEnd", "PreToolUse", "PostToolUse", "PostToolUseFailure"})
        for event, groups in self.settings.items():
            self.assertEqual(len(groups), 1)
            group = groups[0]
            self.assertEqual(len(group["hooks"]), 1)
            hook = group["hooks"][0]
            self.assertEqual(hook["type"], "command")
            self.assertEqual(hook["timeout"], 10)
            self.assertFalse(hook.get("async", False))
            if event not in {"SessionStart", "SessionEnd"}:
                self.assertEqual(group["matcher"], "AskUserQuestion")

    def test_success_preserves_arguments_stdin_and_silence(self):
        for shell in self.shells:
            for event in self.settings:
                with self.subTest(shell=shell, event=event):
                    result, payload = self.run_hook(shell, event, self.env)
                    self.assertEqual(result.returncode, 0, result.stderr)
                    self.assertEqual(result.stdout, "")
                    self.assertEqual(result.stderr, "")
                    self.assertEqual((self.base / "stdin").read_text(), payload)
                    self.assertEqual((self.base / "args").read_text().splitlines(), [
                        "--root", self.env["DELIVER_ROOT"], "claude-hook",
                        "--role", "gm", "--pane", "wC:p5",
                    ])

    def test_missing_or_empty_environment_blocks(self):
        for shell in self.shells:
            for event in self.settings:
                for name in ["HRELAY_GUARD_BIN", "DELIVER_ROOT", "HERDR_RELAY_FROM", "HERDR_PANE_ID"]:
                    for missing in [False, True]:
                        with self.subTest(shell=shell, event=event, name=name, missing=missing):
                            env = dict(self.env)
                            if missing:
                                env.pop(name)
                            else:
                                env[name] = ""
                            result, _ = self.run_hook(shell, event, env)
                            self.assertEqual(result.returncode, 2, result.stderr)
                            self.assertEqual(result.stdout, "")
                            self.assertFalse((self.base / "args").exists())

    def test_launcher_and_cli_failures_become_exit_two(self):
        no_exec = self.base / "not executable"
        no_exec.write_text("#!/bin/sh\nexit 0\n")
        no_exec.chmod(0o600)
        for shell in self.shells:
            for event in self.settings:
                for binary in [self.base / "missing binary", no_exec]:
                    with self.subTest(shell=shell, event=event, binary=binary):
                        env = dict(self.env, HRELAY_GUARD_BIN=str(binary))
                        result, _ = self.run_hook(shell, event, env)
                        self.assertEqual(result.returncode, 2, result.stderr)
                        self.assertEqual(result.stdout, "")
                for code in [1, 2, 126, 127]:
                    with self.subTest(shell=shell, event=event, code=code):
                        result, _ = self.run_hook(shell, event, dict(self.env, FAKE_EXIT=str(code)))
                        self.assertEqual(result.returncode, 2, result.stderr)
                        self.assertEqual(result.stdout, "")


if __name__ == "__main__":
    unittest.main()
