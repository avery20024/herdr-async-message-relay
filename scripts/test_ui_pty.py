"""以私人 Unix socket 與 PTY 驗證 UI；不啟動 relay 或連接正式佇列。"""
import errno
import fcntl
import http.server
import json
import os
from pathlib import Path
import pty
import re
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
from urllib.parse import parse_qs, urlsplit


class UIHandler(http.server.BaseHTTPRequestHandler):
    def guard_response(self):
        body = json.dumps({"project_root": self.server.root,
                           "policy": {"version": 1, "enabled": self.server.guard_enabled.get(self.server.root, True)},
                           "observations": [{"role": "gm", "status": "uninitialized", "pending_count": 0}]}).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_PUT(self):
        self.server.guard_requests.append((self.command, self.path))
        if self.path != "/v1/claude-guard":
            self.send_error(404)
            return
        body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        self.server.guard_enabled[self.server.root] = body["enabled"]
        self.guard_response()

    def do_GET(self):
        if self.path == "/v1/claude-guard":
            self.server.guard_requests.append((self.command, self.path))
            self.guard_response()
            return
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
        self.preferences = Path(self.temp.name) / "ui-preferences.json"
        # 與 Config.Socket 的命名保持一致，避免測試意外碰到其他 socket。
        path = self.state / "deliver.sock"
        self.assertFalse(path.exists())
        self.server = socketserver.UnixStreamServer(str(path), UIHandler)
        self.server.root = str(self.root)
        self.server.requests = []
        self.server.guard_requests = []
        self.server.guard_enabled = {}
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

    def launch(self, rows=30, columns=180, needle=b"PTY original body"):
        fd, slave = pty.openpty()
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", rows, columns, 0, 0))
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
                       "DELIVER_STATE_DIR": str(self.state), "LANG": "en_US.UTF-8",
                       "HERDR_RELAY_UI_CONFIG": str(self.preferences)}
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
        self.wait_for(needle)

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

    def wait_for_text(self, needle, timeout=8):
        # 末欄字元會被renderer的自動換行開關分隔；只去CSI，不放寬文字本身。
        def visible():
            return re.sub(rb"\x1b\[[0-?]*[ -/]*[@-~]", b"", bytes(self.output))
        until = time.monotonic() + timeout
        while needle not in visible() and time.monotonic() < until:
            self.pump(0.05)
        self.assertIn(needle, visible(), bytes(self.output[-2000:]))

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

    def test_shortcuts_help_and_dialog_drafts(self):
        self.launch(columns=168)
        count = len(self.server.requests)
        os.write(self.fd, b"?")
        self.wait_for_text(b"Keyboard shortcuts")
        self.wait_for_text(b"Ctrl+F")
        self.wait_for_text(b"Ctrl+P")
        self.wait_for_text(b"ESC close")
        os.write(self.fd, b"q\x12")  # help 內 q 不退出，Ctrl+R 不查訊息。
        self.pump(0.3)
        self.assertEqual(len(self.server.requests), count)
        self.assertFalse(self.preferences.exists())
        os.write(self.fd, b"?")
        self.pump(0.2)
        self.output.clear()
        os.write(self.fd, b"\x06")
        self.wait_for_text(b"Filters")
        os.write(self.fd, b"\x1b[C")  # sender=dev 草稿。
        self.pump(0.15)
        self.output.clear()
        os.write(self.fd, b"?")
        self.wait_for_text(b"Keyboard shortcuts")
        self.output.clear()
        os.write(self.fd, b"\x1b")
        # 說明外footer現為全空白，關閉後欄標完整重繪；選取列可能在較窄modal外而不重輸。
        self.wait_for_text(b"Filters 1/3")
        self.assertEqual(len(self.server.requests), count)
        os.write(self.fd, b"\r")
        self.pump(0.3)
        self.assertTrue(any("sender=dev" in path for _, path in self.server.requests))
        count = len(self.server.requests)
        os.write(self.fd, b"\x10")
        self.pump(0.2)
        os.write(self.fd, b"\x1b[Cj\x1b[C")  # en、Latte 草稿。
        self.pump(0.2)
        self.output.clear()
        os.write(self.fd, b"?")
        self.wait_for_text(b"Keyboard shortcuts")
        self.output.clear()
        os.write(self.fd, b"?")
        # 說明關閉後footer完整重繪；草稿內容仍由下方實際保存值核對。
        self.wait_for_text(b"Preferences 2/3")
        self.assertFalse(self.preferences.exists())
        os.write(self.fd, b"\r")
        self.pump(0.3)
        saved = json.loads(self.preferences.read_text())
        self.assertEqual((saved["language"], saved["theme"]), ("en", "catppuccin-latte"))
        self.assertEqual(len(self.server.requests), count)
        os.write(self.fd, b"?")
        self.pump(0.2)
        self.finish(b"\x03")

    def test_shortcuts_help_minimum_resize_and_exit(self):
        self.server.items = [{"seq": 1, "id": "min-help", "sender": "dev", "recipient": "gm",
                              "state": "queued", "body": "MINREADY", "created_at": 1792771200000}]
        self.launch(rows=10, columns=20, needle=b"MINREADY")
        count = len(self.server.requests)
        os.write(self.fd, b"?")
        self.wait_for_text(b"ESC close")
        os.write(self.fd, b"dG")
        self.pump(0.2)
        fcntl.ioctl(self.slave, termios.TIOCSWINSZ, struct.pack("HHHH", 30, 100, 0, 0))
        os.kill(self.child, signal.SIGWINCH)
        self.pump(0.3)
        os.write(self.fd, b"g")
        self.wait_for_text(b"Select next/previous message")
        self.assertEqual(len(self.server.requests), count)
        fcntl.ioctl(self.slave, termios.TIOCSWINSZ, struct.pack("HHHH", 9, 19, 0, 0))
        os.kill(self.child, signal.SIGWINCH)
        self.wait_for_text(b"Minimum")
        self.finish(b"q")

    def test_keyboard_filter_refresh_resize_and_quit(self):
        self.launch()
        count = len(self.server.requests)
        self.pump(1.2)
        self.assertEqual(len(self.server.requests), count, "default mode must be manual")
        os.write(self.fd, b"r")
        self.pump(0.3)
        self.assertGreater(len(self.server.requests), count)
        os.write(self.fd, b"\x06")  # Ctrl+F
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
        self.wait_for(b"BODY-FIRST-24")  # 單列footer釋出一列，上半12筆可見，半頁6筆。
        self.output.clear()
        os.write(self.fd, b"u")
        self.wait_for(b"BODY-FIRST-30")
        self.finish(b"q")

    def test_gg_and_uppercase_g_jump_loaded_endpoints(self):
        self.server.items = [
            {"seq": seq, "id": f"jump-{seq}", "sender": "dev", "recipient": "gm",
             "state": "queued", "created_at": 1792771200000,
             "body": f"JUMP-BODY-{seq}"}
            for seq in (3, 2, 1)
        ]
        self.launch(needle=b"JUMP-BODY-3")
        count = len(self.server.requests)
        self.output.clear()
        os.write(self.fd, b"G")
        self.pump(0.2)
        # 強制完整重畫，避免 renderer 省略計數及訊息的共同前綴。
        self.output.clear()
        fcntl.ioctl(self.slave, termios.TIOCSWINSZ, struct.pack("HHHH", 30, 181, 0, 0))
        os.kill(self.child, signal.SIGWINCH)
        self.wait_for_text(b"3/3")
        self.output.clear()
        os.write(self.fd, b"g")
        self.pump(0.2)
        self.assertNotIn(b"1/3", self.output, "single g must not jump")
        os.write(self.fd, b"g")
        self.pump(0.2)
        self.output.clear()
        fcntl.ioctl(self.slave, termios.TIOCSWINSZ, struct.pack("HHHH", 30, 180, 0, 0))
        os.kill(self.child, signal.SIGWINCH)
        self.wait_for_text(b"1/3")
        self.assertEqual(len(self.server.requests), count, "endpoint keys must not fetch")
        self.finish(b"q")

    def test_mouse_wheel_routes_by_screen_coordinates(self):
        self.server.items = [
            {"seq": seq, "id": f"wheel-{seq}", "sender": "dev", "recipient": "gm",
             "state": "queued", "created_at": 1792771200000,
             "body": f"WHEEL-FIRST-{seq}\nSECOND-LINE-{seq}\n" + "\n".join(f"ROW-{n}" for n in range(60)),
             "idempotency_key": "long-key-" * 150}
            for seq in (2, 1)
        ]
        self.launch(needle=b"WHEEL-FIRST-2")
        self.wait_for(b"\x1b[?1006h")
        count = len(self.server.requests)
        columns = 180

        def redraw_after(event):
            nonlocal columns
            os.write(self.fd, event)
            self.pump(0.15)
            self.output.clear()
            columns = 181 if columns == 180 else 180
            fcntl.ioctl(self.slave, termios.TIOCSWINSZ, struct.pack("HHHH", 30, columns, 0, 0))
            os.kill(self.child, signal.SIGWINCH)
            self.pump(0.25)

        # SGR 滑鼠座標為一起算；下區從第16行開始，中央分隔約在第73欄。
        redraw_after(b"\x1b[<65;120;18M")
        self.wait_for_text(b"Message body")
        self.wait_for_text(b"line 2/62")
        self.wait_for_text("Details · line 1/".encode())
        self.assertIn(b"SECOND-LINE-2", self.output)
        redraw_after(b"\x1b[<65;10;18M")
        self.wait_for_text(b"Details")
        self.wait_for_text(b"line 2/")
        self.wait_for_text("Message body · line 2/62".encode())
        self.assertIn(b"SECOND-LINE-2", self.output)
        self.assertEqual(len(self.server.requests), count)
        redraw_after(b"\x1b[<65;10;4M")
        self.wait_for_text(b"2/2")
        self.wait_for_text("Message body · line 1/62".encode())
        self.wait_for_text("Details · line 1/".encode())
        self.assertEqual(len(self.server.requests), count)
        self.finish(b"q")
        self.assertIn(b"\x1b[?1006l", self.output)
        self.assertIn(b"\x1b[?1002l", self.output)

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
        self.wait_for(b"META-04")  # 左下資料頁高11行；前7行是欄位與證據首行。
        self.output.clear()
        os.write(self.fd, b"]" * 6)
        # 與其他行不共用META-前綴，避免終端差分重繪省略相同字元。
        self.wait_for(b"END-OF-METADATA-REACHABLE")
        self.output.clear()
        os.write(self.fd, b"[" * 6)
        self.wait_for(b"META-FIRST")
        self.assertEqual(len(self.server.requests), count, "metadata paging must not fetch or mutate")
        self.finish(b"q")

    def test_mobile_cards_navigation_filters_and_resize(self):
        self.server.items = [
            {"seq": seq, "id": f"mobile-{seq}", "sender": "dev", "recipient": "gm",
             "state": "queued", "created_at": 1792771200000,
             "body": f"READY\nFIRST{seq}\n" + "\n".join(f"B{seq}-{n:02}" for n in range(60)),
             "batch_detail": {"String": "\n".join(f"Z6-{n:02}" for n in range(20)) + "\nPHONE-END", "Valid": True}}
            for seq in range(30, 0, -1)
        ]
        self.launch(rows=20, columns=40, needle=b"READY")
        count = len(self.server.requests)
        self.assertNotIn(b"Enlarge terminal", self.output)
        os.write(self.fd, b"D")
        self.pump(0.2)
        # Resize強制完整重畫；不以差分省略的共用前綴誤判按鍵失效。
        self.output.clear()
        fcntl.ioctl(self.slave, termios.TIOCSWINSZ, struct.pack("HHHH", 20, 41, 0, 0))
        os.kill(self.child, signal.SIGWINCH)
        self.wait_for(b"B30-06")
        os.write(self.fd, b"U")
        self.pump(0.2)
        self.output.clear()
        os.write(self.fd, b"d")
        self.pump(0.2)
        fcntl.ioctl(self.slave, termios.TIOCSWINSZ, struct.pack("HHHH", 20, 40, 0, 0))
        os.kill(self.child, signal.SIGWINCH)
        self.wait_for(b"FIRST28")  # 單列footer後可見4張完整卡片，半頁2筆。
        self.output.clear()
        for _ in range(20):
            os.write(self.fd, b"]")
            self.pump(0.08)
        self.wait_for(b"PHONE-END")
        self.assertEqual(len(self.server.requests), count, "phone scrolling must not issue requests")
        os.write(self.fd, b"\x06")
        self.wait_for_text(b"Enter Esc")
        os.write(self.fd, b"\x1b[C\r")
        self.pump(0.3)
        self.assertTrue(any("sender=dev" in path for _, path in self.server.requests))
        self.output.clear()
        fcntl.ioctl(self.slave, termios.TIOCSWINSZ, struct.pack("HHHH", 10, 20, 0, 0))
        os.kill(self.child, signal.SIGWINCH)
        self.wait_for(b"READY")
        self.assertNotIn(b"Minimum", self.output)
        os.write(self.fd, b"\x06")
        self.wait_for_text(b"Enter Esc")
        os.write(self.fd, b"\x1b")
        self.pump(0.2)
        self.output.clear()
        fcntl.ioctl(self.slave, termios.TIOCSWINSZ, struct.pack("HHHH", 16, 100, 0, 0))
        os.kill(self.child, signal.SIGWINCH)
        self.wait_for(b"Sender")  # 100欄回到桌面六欄。
        self.finish(b"q")
        self.assertTrue(all(method == "GET" for method, _ in self.server.requests))

    def test_shift_p_and_d_clear_filter_drafts(self):
        self.server.items = [{"seq": 1, "id": "reset-pty", "sender": "dev", "recipient": "gm",
                              "state": "queued", "created_at": 1792771200000,
                              "body": "PTY original body 中文\n" + "\n".join(f"RESET-{n:02}" for n in range(60))}]
        self.launch()

        def query():
            return parse_qs(urlsplit(self.server.requests[-1][1]).query)

        os.write(self.fd, b"\x06")
        self.wait_for(b"Enter apply")
        os.write(self.fd, b"\x1b[C\x1b[B\x1b[C\x1b[B\x1b[C\r")
        self.pump(0.4)
        self.assertEqual({k: query().get(k) for k in ("sender", "recipient", "state")},
                         {"sender": ["dev"], "recipient": ["gm"], "state": ["queued"]})
        count = len(self.server.requests)
        os.write(self.fd, b"\x06\x1b[Bd")  # 選中recipient後d，只清草稿。
        self.pump(0.2)
        self.assertEqual(len(self.server.requests), count)
        os.write(self.fd, b"\r")
        self.pump(0.3)
        self.assertNotIn("recipient", query())
        self.assertEqual(query().get("sender"), ["dev"])
        self.assertEqual(query().get("state"), ["queued"])
        count = len(self.server.requests)
        os.write(self.fd, b"\x06P\x1b")  # Shift+P清草稿後Esc，不套用。
        self.pump(0.3)
        self.assertEqual(len(self.server.requests), count)
        os.write(self.fd, b"D")  # 仍翻本文，不清已套用的篩選。
        self.pump(0.2)
        self.assertEqual(len(self.server.requests), count)
        os.write(self.fd, b"P")  # 主畫面立即清除全部篩選。
        self.pump(0.4)
        self.assertGreater(len(self.server.requests), count)
        self.assertFalse(any(k in query() for k in ("sender", "recipient", "state")))
        count = len(self.server.requests)
        os.write(self.fd, b"P")
        self.pump(0.2)
        self.assertEqual(len(self.server.requests), count, "already-clear P is a no-op")
        os.write(self.fd, b"\x06\x1b[C\r")
        self.pump(0.3)
        self.assertEqual(query().get("sender"), ["dev"])
        os.write(self.fd, b"\x06P\r")  # 選單P全清後Enter才套用。
        self.pump(0.3)
        self.assertFalse(any(k in query() for k in ("sender", "recipient", "state")))
        self.finish(b"q")
        self.assertTrue(all(method == "GET" for method, _ in self.server.requests))

    def test_preferences_persist_cross_root_and_manual_reload(self):
        self.launch()
        count = len(self.server.requests)
        os.write(self.fd, b"\x10")  # Ctrl+P只開Preferences。
        self.wait_for(b"Preferences")
        os.write(self.fd, b"\x1b[C\x1b[C\x1b[B\x1b[C\r")  # 繁中＋Latte，Enter儲存。
        self.pump(0.3)
        self.assertEqual(json.loads(self.preferences.read_text()),
                         {"version": 1, "language": "zh-TW", "theme": "catppuccin-latte"})
        self.wait_for("P 清除篩選".encode())
        self.assertEqual(len(self.server.requests), count)
        self.output.clear()
        self.preferences.write_text(json.dumps({"version": 1, "language": "en", "theme": "vesper"}))
        self.pump(0.3)
        self.assertNotIn(b"Preferences reloaded", self.output, "must not proactively reload")
        os.write(self.fd, b"\x12")  # Ctrl+R只讀設定，不刷新訊息。
        self.wait_for(b"Preferences reloaded.")
        self.assertEqual(len(self.server.requests), count)
        self.output.clear()
        self.preferences.write_text(json.dumps({"version": 1, "language": "zh-TW", "theme": "rose-pine"}))
        os.write(self.fd, b"\x10")  # 開Preferences也重新載入。
        self.wait_for("偏好設定".encode())
        self.wait_for("Rosé Pine".encode())
        os.write(self.fd, b"\x1b")
        self.pump(0.2)
        self.finish(b"q")
        os.close(self.fd)
        self.fd = None
        self.output.clear()
        self.root = Path(self.temp.name).resolve() / "second-team"
        (self.root / "角色" / "gm").mkdir(parents=True)
        self.server.root = str(self.root)
        self.launch()  # 同設定路徑／不同root，啟動直接沿用繁中。
        self.wait_for("發件者".encode())
        os.write(self.fd, b"\x10")
        self.wait_for("Rosé Pine".encode())
        self.finish(b"\x03")
        self.assertTrue(all(method == "GET" for method, _ in self.server.requests))

    def test_preferences_minimum_save_and_corrupt_reload(self):
        self.server.items = [{"seq": 1, "id": "prefs-tiny", "sender": "dev", "recipient": "gm",
                              "state": "queued", "created_at": 1792771200000, "body": "READY"}]
        self.launch(rows=10, columns=20, needle=b"READY")
        count = len(self.server.requests)
        os.write(self.fd, b"\x10")
        self.wait_for_text(b"Preferences")
        os.write(self.fd, b"\x1b[B\x1b[C")
        self.wait_for_text(b"Latte")
        os.write(self.fd, b"\r")
        self.pump(0.3)
        self.assertEqual(json.loads(self.preferences.read_text())["theme"], "catppuccin-latte")
        self.preferences.write_text("invalid preferences")
        self.output.clear()
        os.write(self.fd, b"\x12")
        self.wait_for_text(b"! CFG")
        self.assertEqual(len(self.server.requests), count)
        os.write(self.fd, b"\x10\x1b[C\r")  # 損毀時不可覆寫，保留現有theme及draft。
        self.pump(0.3)
        self.assertEqual(self.preferences.read_text(), "invalid preferences")
        self.finish(b"\x03")

    def test_team_guard_default_toggle_cancel_and_reload(self):
        self.launch()
        count = len(self.server.requests)
        os.write(self.fd, b"\x10")
        self.wait_for_text(b"On")
        os.write(self.fd, b"jj\x1b[C")  # 第三欄關閉草稿。
        self.pump(0.2)
        os.write(self.fd, b"\x1b")
        self.pump(0.2)
        self.assertNotIn(self.server.root, self.server.guard_enabled)
        self.output.clear()
        os.write(self.fd, b"\x10")
        self.wait_for_text(b"On")
        os.write(self.fd, b"jj\x1b[C\r")
        self.pump(0.4)
        self.assertIs(self.server.guard_enabled[self.server.root], False)
        self.assertEqual(sum(method == "PUT" for method, _ in self.server.guard_requests), 1)
        self.assertEqual(len(self.server.requests), count, "policy editing must not fetch message history")
        self.output.clear()
        os.write(self.fd, b"\x10")
        self.wait_for_text(b"Off")
        self.finish(b"\x03")

    def test_ctrl_c_restores_terminal(self):
        self.launch()
        self.finish(b"\x03")
