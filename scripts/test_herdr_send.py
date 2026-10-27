#!/usr/bin/env python3
"""以隔離 PATH 驗證明確身分與不依賴語言目錄的相容入口。"""
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
        for key in ("DELIVER_ROOT", "HERDR_RELAY_FROM", "HERDR_SEND_FROM", "HERDR_SEND_IDEMPOTENCY_KEY"):
            self.env.pop(key, None)
        self.env["HERDR_RELAY_FROM"] = "dev"

    def tearDown(self):
        self.temp.cleanup()

    def run_send(self, args=None, code=0, cwd=None):
        result = subprocess.run(["/bin/bash", str(self.script)] +
                                (["gm", "訊息\n第二行 'quotes' $(touch never)"] if args is None else args),
                                env=self.env, cwd=cwd or self.root,
                                capture_output=True, text=True, timeout=5)
        self.assertEqual(result.returncode, code, (result.stdout, result.stderr))
        return result

    def calls(self):
        path = self.root / "calls"
        return [json.loads(line) for line in path.read_text().splitlines()] if path.exists() else []

    def test_native_short_send_and_exact_body(self):
        self.run_send()
        self.assertEqual(self.calls(), [["--root", str(self.root), "send", "--from", "dev", "gm", "訊息\n第二行 'quotes' $(touch never)"]])
        self.assertFalse((self.root / "never").exists())

    def test_no_layout_or_online_seat_required(self):
        self.run_send(args=["offline", "queued"])
        self.assertEqual(self.calls()[0][-2:], ["offline", "queued"])
        self.assertFalse((self.root / "角色").exists())

    def test_explicit_sender_overrides_launch_environment(self):
        self.env["HERDR_SEND_FROM"] = "reviewer"
        self.run_send()
        self.assertEqual(self.calls()[0][3:5], ["--from", "reviewer"])

    def test_cwd_does_not_guess_sender(self):
        del self.env["HERDR_RELAY_FROM"]
        self.run_send(code=1)
        self.assertEqual(self.calls(), [])

    def test_bad_arguments(self):
        for args in ([], ["gm"], ["gm", ""], ["", "x"], ["gm", "x", "--now"]):
            self.run_send(args=args, code=1)
        self.assertEqual(self.calls(), [])

    def test_idempotency_flag_precedes_positional_arguments(self):
        self.env["HERDR_SEND_IDEMPOTENCY_KEY"] = "task-123-report-1"
        self.run_send()
        self.assertEqual(self.calls()[0][5:8], ["--key", "task-123-report-1", "gm"])

    def test_missing_path_command_has_no_fallback(self):
        self.client.unlink()
        result = self.run_send(code=1)
        self.assertIn("hrelay not found", result.stderr)
        self.assertEqual(self.calls(), [])

    def test_explicit_root_is_used(self):
        self.env["DELIVER_ROOT"] = str(self.root / "a different root")
        self.run_send()
        self.assertEqual(self.calls()[0][1], self.env["DELIVER_ROOT"])

    def test_multilingual_names_preserved_for_native_validation(self):
        self.env["HERDR_RELAY_FROM"] = "担当A"
        self.run_send(args=["Réviseur", "test"])
        self.assertEqual(self.calls()[0][3:], ["--from", "担当A", "Réviseur", "test"])

    def test_client_exit_code_is_preserved(self):
        self.env["MOCK_CODE"] = "9"
        self.run_send(code=9)

    def test_legacy_binary_overrides_are_not_used(self):
        self.env["HERDR_RELAY_BIN"] = "/nonexistent/old-client"
        self.env["HERDR_DELIVER_BIN"] = "/nonexistent/old-client"
        self.run_send()
        self.assertEqual(len(self.calls()), 1)


if __name__ == "__main__":
    unittest.main()
