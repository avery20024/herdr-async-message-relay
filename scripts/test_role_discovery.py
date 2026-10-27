#!/usr/bin/env python3
"""隔離 CLI／worker 整合測試；Herdr 為測試替身，不操作真實 AI 或正式佇列。"""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import unittest

REPO = Path(__file__).resolve().parents[1]


class RoleDiscoveryTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.build = tempfile.TemporaryDirectory(prefix="hr-build-", dir="/tmp")
        cls.binary = Path(cls.build.name) / "hrelay"
        try:
            subprocess.run(["go", "build", "-o", str(cls.binary), "./cmd/hrelay"], cwd=REPO, check=True, timeout=120)
        except BaseException:
            cls.build.cleanup()
            raise

    @classmethod
    def tearDownClass(cls):
        cls.build.cleanup()

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="hr-disc-", dir="/tmp")
        self.addCleanup(self.temp.cleanup)
        self.base = Path(self.temp.name)
        self.root = self.base / "team"
        self.root.mkdir()
        self.state = self.base / "state"
        self.state.mkdir()
        self.agents = []
        for index, relative in enumerate(("agents/alpha", "équipe/bravo", "開発/担当A"), 1):
            directory = self.root / relative
            directory.mkdir(parents=True)
            (directory / "AGENT.md").write_text("# Isolated test role\nNo production access.\n")
            self.agents.append(dict(cwd="/irrelevant", foreground_cwd=str(directory), pane_id=f"wT:p{index}",
                                    agent_status="idle", state_change_seq=1,
                                    agent_session=dict(agent="pi", kind="path", value=f"test-{index}")))
        self.save_agents()
        mock = self.base / "herdr"
        mock.write_text(f'''#!{sys.executable}
import json, os, pathlib, sys
base = pathlib.Path(os.environ["TEST_DISCOVERY_BASE"])
if sys.argv[1:3] == ["agent", "list"]:
    print(json.dumps({{"result": {{"agents": json.loads((base / "agents.json").read_text())}}}}))
elif sys.argv[1:3] == ["agent", "prompt"]:
    with (base / "prompts.jsonl").open("a") as f:
        f.write(json.dumps({{"pane": sys.argv[3], "body": sys.argv[4]}}) + "\\n")
    print('{{"result":{{"agent_status":"working"}}}}')
else:
    sys.exit(7)
''')
        mock.chmod(0o700)
        self.env = dict(os.environ, DELIVER_STATE_DIR=str(self.state), HERDR_BIN_PATH=str(mock),
                        TEST_DISCOVERY_BASE=str(self.base), DELIVER_POLL_MS="50", HERDR_RELAY_FROM="alpha")
        for key in ("DELIVER_BATCH_SIZE", "DELIVER_BATCH_BYTES", "DELIVER_RETRY_BACKOFF_MS", "DELIVER_MAX_ATTEMPTS"):
            self.env.pop(key, None)
        self.log = (self.base / "worker.log").open("w")
        self.addCleanup(self.log.close)
        self.worker = subprocess.Popen([str(self.binary), "--root", str(self.root), "serve"], env=self.env,
                                       stdout=self.log, stderr=self.log)
        self.addCleanup(self.stop_worker)
        self.wait_for(lambda: (self.state / "deliver.sock").exists())

    def stop_worker(self):
        if self.worker.poll() is None:
            self.worker.terminate()
            try:
                self.worker.wait(timeout=5)
            except subprocess.TimeoutExpired:
                self.worker.kill()
                self.worker.wait(timeout=5)

    def save_agents(self):
        staging = self.base / "agents.tmp"
        staging.write_text(json.dumps(self.agents))
        staging.replace(self.base / "agents.json")

    def cli(self, *args):
        result = subprocess.run([str(self.binary), "--root", str(self.root), *args], env=self.env,
                                check=True, capture_output=True, text=True, timeout=10)
        return json.loads(result.stdout) if result.stdout.strip() else None

    def wait_for(self, predicate):
        deadline = time.monotonic() + 8
        while time.monotonic() < deadline:
            if predicate():
                return
            if hasattr(self, "worker") and self.worker.poll() is not None:
                self.fail((self.base / "worker.log").read_text())
            time.sleep(0.05)
        self.fail("timed out waiting for isolated worker")

    def send(self, role):
        return self.cli("send", "--ensure=false", role, "isolated test")['id']

    def message_state(self, message_id):
        return self.cli("show", message_id)['state']

    def test_three_language_independent_roles_deliver(self):
        ids = [self.send(role) for role in ("alpha", "bravo", "担当A")]
        self.wait_for(lambda: all(self.message_state(i) == "submitted" for i in ids))
        prompts = [json.loads(line) for line in (self.base / "prompts.jsonl").read_text().splitlines()]
        self.assertEqual({p['pane'] for p in prompts}, {"wT:p1", "wT:p2", "wT:p3"})
        self.assertFalse((self.root / "角色").exists())

    def test_missing_policy_defaults_off_but_explicit_enable_is_respected(self):
        self.agents[0]['agent_session']['agent'] = 'claude'
        self.save_agents()
        first = self.send('alpha')
        self.wait_for(lambda: self.message_state(first) == 'submitted')
        self.assertFalse((self.state / 'claude-guard' / 'team-policy.json').exists())
        self.cli('claude-guard', '--team', 'enable')
        self.agents[0]['state_change_seq'] = 2
        self.save_agents()
        second = self.send('alpha')
        time.sleep(0.3)
        self.assertEqual(self.message_state(second), 'queued')
        self.cli('claude-guard', '--team', 'disable')
        self.wait_for(lambda: self.message_state(second) == 'submitted')
        self.assertFalse((self.state / 'claude-guard' / 'alpha').exists())

    def test_conflict_stays_queued_then_recovers(self):
        duplicate = dict(self.agents[0], pane_id="wT:p9")
        self.agents.append(duplicate)
        self.save_agents()
        message_id = self.send("alpha")
        time.sleep(0.3)
        self.assertEqual(self.message_state(message_id), "queued")
        self.agents.pop()
        self.save_agents()
        self.wait_for(lambda: self.message_state(message_id) == "submitted")

    def test_restart_keeps_queue_and_discovers_new_seat(self):
        message_id = self.send("late")
        self.assertEqual(self.message_state(message_id), "queued")
        self.stop_worker()
        directory = self.root / "new" / "late"
        directory.mkdir(parents=True)
        self.agents.append(dict(self.agents[0], foreground_cwd=str(directory), pane_id="wT:p4"))
        self.save_agents()
        self.worker = subprocess.Popen([str(self.binary), "--root", str(self.root), "serve"], env=self.env,
                                       stdout=self.log, stderr=self.log)
        self.wait_for(lambda: (self.state / "deliver.sock").exists())
        self.wait_for(lambda: self.message_state(message_id) == "submitted")
        prompts = [json.loads(line) for line in (self.base / "prompts.jsonl").read_text().splitlines()]
        self.assertEqual(len(prompts), 1)
        self.assertEqual(prompts[0]['pane'], "wT:p4")

    def test_outside_root_and_cwd_only_never_deliver(self):
        for agent in self.agents:
            agent['cwd'] = agent['foreground_cwd']
            agent['foreground_cwd'] = str(self.base)
        self.save_agents()
        message_id = self.send("alpha")
        time.sleep(0.3)
        self.assertEqual(self.message_state(message_id), "queued")
        self.assertFalse((self.base / "prompts.jsonl").exists())


if __name__ == "__main__":
    unittest.main()
