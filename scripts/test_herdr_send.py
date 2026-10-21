#!/usr/bin/env python3
"""以封閉 PATH 的假原生 hrelay 驗證角色推導，不通知真實座位。"""
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

SCRIPT = Path(__file__).resolve().parents[1] / "examples/herdr-send.sh"


class SendTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name).resolve()
        (self.root / "scripts").mkdir()
        for role in ("dev", "gm"):
            (self.root / "角色" / role).mkdir(parents=True)
        self.bin = self.root / "bin"
        self.bin.mkdir()
        (self.bin / "dirname").symlink_to(shutil.which("dirname"))
        self.client = self.bin / "hrelay"
        self.client.write_text(f'''#!{sys.executable}
import json, os, pathlib, sys
root = pathlib.Path(os.environ["MOCK_ROOT"])
with (root / "calls").open("a") as f: f.write(json.dumps(sys.argv[1:]) + "\\n")
print('{{"accepted":true,"id":"msg-test","state":"queued"}}')
sys.exit(int(os.environ.get("MOCK_CODE", "0")))
''')
        self.client.chmod(0o755)
        self.script = self.root / "scripts/herdr-send.sh"
        shutil.copyfile(SCRIPT, self.script)
        self.env = dict(os.environ, PATH=str(self.bin), MOCK_ROOT=str(self.root))
        for key in ("HERDR_SEND_FROM", "HERDR_RELAY_BIN", "HERDR_DELIVER_BIN", "HERDR_SEND_IDEMPOTENCY_KEY"):
            self.env.pop(key, None)

    def tearDown(self):
        self.temp.cleanup()

    def run_send(self, args=None, code=0, cwd=None):
        result = subprocess.run(["/bin/bash", str(self.script)] +
                                (["gm", "訊息\n第二行 'quotes' $(touch never)"] if args is None else args),
                                env=self.env, cwd=cwd or self.root / "角色/dev",
                                capture_output=True, text=True, timeout=5)
        self.assertEqual(result.returncode, code, (result.stdout, result.stderr))
        return result

    def calls(self):
        path = self.root / "calls"
        return [json.loads(line) for line in path.read_text().splitlines()] if path.exists() else []

    def test_native_short_send_and_exact_body(self):
        self.run_send()
        self.assertEqual(self.calls(), [["--root", str(self.root), "send", "--from", "dev", "gm", "訊息\n第二行 'quotes' $(touch never)"]])
        self.assertFalse((self.root / "角色/dev/never").exists())

    def test_role_subdirectory(self):
        sub = self.root / "角色/dev/sub"
        sub.mkdir()
        self.run_send(cwd=sub)
        self.assertEqual(self.calls()[0][3:5], ["--from", "dev"])

    def test_explicit_sender_from_root(self):
        self.env["HERDR_SEND_FROM"] = "dev"
        self.run_send(cwd=self.root)
        self.assertEqual(self.calls()[0][3:5], ["--from", "dev"])

    def test_root_does_not_guess_sender(self):
        self.run_send(cwd=self.root, code=1)
        self.assertEqual(self.calls(), [])

    def test_bad_arguments_and_roles(self):
        for args in ([], ["gm"], ["gm", ""], ["../gm", "x"], ["missing", "x"], ["gm", "x", "--now"]):
            self.run_send(args=args, code=1)
        for sender in ("../dev", "missing"):
            self.env["HERDR_SEND_FROM"] = sender
            self.run_send(code=1)
        self.assertEqual(self.calls(), [])

    def test_idempotency_flag_precedes_positional_arguments(self):
        self.env["HERDR_SEND_IDEMPOTENCY_KEY"] = "task-123-report-1"
        self.run_send()
        self.assertEqual(self.calls()[0][5:7], ["--key", "task-123-report-1"])
        self.assertEqual(self.calls()[0][7], "gm")

    def test_missing_path_command_does_not_use_project_binary(self):
        local = self.root / "herdr-async-message-relay/bin/hrelay"
        local.parent.mkdir(parents=True)
        self.client.rename(local)
        self.env["HERDR_RELAY_BIN"] = str(local)
        self.env["HERDR_DELIVER_BIN"] = str(local)
        result = self.run_send(code=1)
        self.assertIn("找不到 hrelay", result.stderr)
        self.assertEqual(self.calls(), [])

    def test_legacy_overrides_are_not_used(self):
        self.env["HERDR_RELAY_BIN"] = "/nonexistent/old-client"
        self.env["HERDR_DELIVER_BIN"] = "/nonexistent/legacy-client"
        self.run_send()
        self.assertEqual(len(self.calls()), 1)

    def test_no_plugin_discovery_or_jq_dependency(self):
        for tool in ("herdr", "jq"):
            path = self.bin / tool
            path.write_text(f'''#!{sys.executable}
import pathlib, sys
pathlib.Path({str(self.root / "forbidden-discovery")!r}).touch()
sys.exit(99)
''')
            path.chmod(0o755)
        self.env["HERDR_BIN_PATH"] = str(self.bin / "herdr")
        self.run_send()
        self.assertFalse((self.root / "forbidden-discovery").exists())

    def test_client_exit_code_is_preserved(self):
        self.env["MOCK_CODE"] = "9"
        self.run_send(code=9)
        self.assertEqual(len(self.calls()), 1)

    def test_ambiguous_physical_sender_is_rejected(self):
        (self.root / "角色/gm").rmdir()
        (self.root / "角色/gm").symlink_to(self.root / "角色/dev", target_is_directory=True)
        self.run_send(code=1)
        self.assertEqual(self.calls(), [])


if __name__ == "__main__":
    unittest.main()
