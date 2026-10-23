"""以私人 Unix socket 與 PTY 驗證 UI；不啟動 relay 或連接正式佇列。"""
import errno
import fcntl
import http.server
import json
import os
from pathlib import Path
import pty
import select
import signal
import socketserver
import struct
import subprocess
import tempfile
import termios
import threading
import time
import unittest


class UIHandler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        self.server.requests.append((self.command, self.path))
        body = json.dumps({
            "project_root": self.server.root,
            "items": getattr(self.server, "items", [{"seq": 1, "id": "msg-pty", "sender": "dev", "recipient": "gm",
                       "state": "queued", "body": "PTY original body 中文", "created_at": 1792771200000}]),
            "next_before": 0, "senders": ["dev"], "recipients": ["gm"],
        }).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_POST(self):
        self.server.requests.append((self.command, self.path))
        self.send_error(405)

    def log_message(self, *_):
        pass


@unittest.skipUnless(os.name == "posix", "PTY requires POSIX")
class UITerminalTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.build_dir = tempfile.TemporaryDirectory(prefix="hrelay-ui-build-")
        cls.binary = str(Path(cls.build_dir.name) / "hrelay")
        try:
            subprocess.run(["go", "build", "-o", cls.binary, "./cmd/hrelay"],
                           cwd=Path(__file__).resolve().parents[1], check=True, timeout=120,
                           capture_output=True)
        except Exception:
            cls.build_dir.cleanup()
            raise

    @classmethod
    def tearDownClass(cls):
        cls.build_dir.cleanup()

    def setUp(self):
        # macOS 的 Unix socket 路徑較短，使用 /tmp 並在建立前確認不存在。
        self.temp = tempfile.TemporaryDirectory(prefix="hui-", dir="/tmp")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve() / "team"
        (self.root / "角色" / "gm").mkdir(parents=True)
        self.state = Path(self.temp.name) / "state"
        self.state.mkdir()
        # 與 Config.Socket 的命名保持一致，避免測試意外碰到其他 socket。
        path = self.state / "deliver.sock"
        self.assertFalse(path.exists())
        self.server = socketserver.UnixStreamServer(str(path), UIHandler)
        self.server.root = str(self.root)
        self.server.requests = []
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=False)
        self.thread.start()
        self.addCleanup(self.close_server)
        self.child = None
        self.fd = None
        self.output = bytearray()

    def close_server(self):
        if self.child is not None:
            try:
                os.killpg(self.child, signal.SIGTERM)
            except ProcessLookupError:
                pass
            os.waitpid(self.child, 0)
            self.child = None
        if self.fd is not None:
            os.close(self.fd)
            self.fd = None
        self.server.shutdown()
        self.server.server_close()
        self.thread.join(timeout=5)
        self.assertFalse(self.thread.is_alive())

    def launch(self):
        fd, slave = pty.openpty()
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 30, 180, 0, 0))
        self.original = termios.tcgetattr(slave)
        pid = os.fork()
        if pid == 0:
            try:
                os.close(fd)
                os.setsid()
                fcntl.ioctl(slave, termios.TIOCSCTTY, 0)
                for target in (0, 1, 2):
                    os.dup2(slave, target)
                if slave > 2:
                    os.close(slave)
                env = {"PATH": "/usr/bin:/bin", "TERM": "xterm-256color", "HOME": self.temp.name,
                       "DELIVER_STATE_DIR": str(self.state), "LANG": "en_US.UTF-8"}
                result = subprocess.run([self.binary, "--root", str(self.root), "--lang", "en", "ui"], env=env)
                # macOS 在 session leader 離開後不可再對 slave 查 termios，需在此先查核。
                restored = termios.tcgetattr(0)
                original = list(self.original)
                # PENDIN 是核心的待重送輸入旗標，不是應用程式的 raw／echo 設定。
                restored[3] &= ~getattr(termios, "PENDIN", 0)
                original[3] &= ~getattr(termios, "PENDIN", 0)
                if restored != original:
                    os.write(1, repr((original, restored)).encode())
                    os._exit(126)
                os.write(1, b"PTY-RESTORED\n")
                os._exit(result.returncode)
            finally:
                os._exit(127)
        self.child, self.fd, self.slave = pid, fd, slave
        self.addCleanup(os.close, slave)
        self.wait_for(b"PTY original body")

    def pump(self, duration):
        until = time.monotonic() + duration
        while time.monotonic() < until:
            ready, _, _ = select.select([self.fd], [], [], min(0.05, max(0, until-time.monotonic())))
            if ready:
                try:
                    chunk = os.read(self.fd, 65536)
                    if chunk:
                        self.output.extend(chunk)
                except OSError as exc:
                    if exc.errno != errno.EIO:
                        raise
                    return

    def wait_for(self, needle, timeout=8):
        until = time.monotonic() + timeout
        while needle not in self.output and time.monotonic() < until:
            self.pump(0.05)
        self.assertIn(needle, self.output, bytes(self.output[-2000:]))

    def finish(self, key):
        os.write(self.fd, key)
        until = time.monotonic() + 5
        while time.monotonic() < until:
            self.pump(0.05)
            pid, status = os.waitpid(self.child, os.WNOHANG)
            if pid:
                self.child = None
                self.assertEqual(os.waitstatus_to_exitcode(status), 0, bytes(self.output[-2500:]))
                self.assertIn(b"PTY-RESTORED", self.output)
                self.assertIn(b"\x1b[?1049l", self.output)
                return
        self.fail("UI did not exit")

    def test_keyboard_filter_refresh_resize_and_quit(self):
        self.launch()
        count = len(self.server.requests)
        self.pump(1.2)
        self.assertEqual(len(self.server.requests), count, "default mode must be manual")
        os.write(self.fd, b"r")
        self.pump(0.3)
        self.assertGreater(len(self.server.requests), count)
        os.write(self.fd, b"\x10")  # Ctrl+P
        self.wait_for(b"Enter apply")
        os.write(self.fd, b"\x1b[C\r")  # sender=dev, Enter
        self.pump(0.3)
        self.assertTrue(any("sender=dev" in path for _, path in self.server.requests))
        os.write(self.fd, b"\x17")  # Ctrl+W
        self.pump(1.3)
        self.wait_for(b"Auto (1s)")
        os.write(self.fd, b"\x17")
        self.pump(0.2)
        stopped = len(self.server.requests)
        self.pump(1.2)
        self.assertEqual(len(self.server.requests), stopped)
        fcntl.ioctl(self.slave, termios.TIOCSWINSZ, struct.pack("HHHH", 24, 90, 0, 0))
        os.kill(self.child, signal.SIGWINCH)
        self.pump(0.2)
        self.finish(b"q")
        self.assertTrue(all(method == "GET" for method, _ in self.server.requests))

    def test_lowercase_list_half_page_and_uppercase_body_page(self):
        self.server.items = [
            {"seq": seq, "id": f"msg-{seq}", "sender": "dev", "recipient": "gm",
             "state": "queued", "created_at": 1792771200000,
             "body": "PTY original body 中文\n" + f"BODY-FIRST-{seq}\n" +
                     "\n".join(f"B{seq}-{line:02}" for line in range(60))}
            for seq in range(30, 0, -1)
        ]
        self.launch()
        self.wait_for(b"BODY-FIRST-30")
        count = len(self.server.requests)
        self.output.clear()
        os.write(self.fd, b"D")
        self.wait_for(b"B30-11")  # 下半高度13列，本文從第13個索引繼續。
        self.assertEqual(len(self.server.requests), count)
        self.output.clear()
        os.write(self.fd, b"U")
        self.wait_for(b"BODY-FIRST-30")
        self.output.clear()
        os.write(self.fd, b"d")
        self.wait_for(b"BODY-FIRST-25")  # 上半11筆可見列，半頁5筆。
        self.output.clear()
        os.write(self.fd, b"u")
        self.wait_for(b"BODY-FIRST-30")
        self.finish(b"q")

    def test_brackets_scroll_metadata_to_last_line(self):
        self.server.items = [{
            "seq": 1, "id": "msg-meta", "sender": "dev", "recipient": "gm",
            "state": "queued", "body": "PTY original body 中文", "created_at": 1792771200000,
            "batch_detail": {"String": "META-FIRST\n" + "\n".join(f"META-{n:02}" for n in range(60)) + "\nEND-OF-METADATA-REACHABLE", "Valid": True},
        }]
        self.launch()
        self.wait_for(b"META-FIRST")
        count = len(self.server.requests)
        self.output.clear()
        os.write(self.fd, b"]")
        self.wait_for(b"META-12")
        self.output.clear()
        os.write(self.fd, b"]" * 6)
        # 與其他行不共用META-前綴，避免終端差分重繪省略相同字元。
        self.wait_for(b"END-OF-METADATA-REACHABLE")
        self.output.clear()
        os.write(self.fd, b"[" * 6)
        self.wait_for(b"META-FIRST")
        self.assertEqual(len(self.server.requests), count, "metadata paging must not fetch or mutate")
        self.finish(b"q")

    def test_ctrl_c_restores_terminal(self):
        self.launch()
        self.finish(b"\x03")
