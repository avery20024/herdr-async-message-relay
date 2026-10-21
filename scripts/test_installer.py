"""以假下載器與測試成品驗證安裝；不連網、不安裝或重啟本機服務。"""
import hashlib
import io
import re
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tarfile
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]


class InstallerTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        (self.root / "scripts").mkdir()
        shutil.copyfile(ROOT / "scripts/install-binary.sh", self.root / "scripts/install-binary.sh")
        (self.root / "VERSION").write_text("v0.1.0\n")
        self.downloads = self.root / "downloads"
        self.downloads.mkdir()
        self.tools = self.root / "tools"
        self.tools.mkdir()
        self.env = dict(os.environ, PATH=str(self.tools) + os.pathsep + os.environ["PATH"],
                        TEST_ROOT=str(self.root), TEST_OS="Darwin", TEST_ARCH="arm64")
        self.mock("uname", 'print(os.environ["TEST_OS"] if sys.argv[1] == "-s" else os.environ["TEST_ARCH"])')
        self.mock("curl", '''
root = pathlib.Path(os.environ["TEST_ROOT"])
url = sys.argv[-1]
with (root / "urls").open("a") as f: f.write(url + "\\n")
assert url.startswith("https://github.com/avery20024/herdr-async-message-relay/releases/download/v0.1.0/")
assert "--proto" in sys.argv and "--proto-redir" in sys.argv
if os.environ.get("TEST_DOWNLOAD_FAIL") == "1": sys.exit(22)
shutil.copyfile(root / "downloads" / url.rsplit("/", 1)[1], sys.argv[sys.argv.index("--output") + 1])
''')
        self.asset = "hrelay_darwin_arm64.tar.gz"
        self.archive()

    def tearDown(self):
        self.temp.cleanup()

    def mock(self, name, code):
        path = self.tools / name
        path.write_text(f"#!{sys.executable}\nimport os, pathlib, shutil, sys\n" + code + "\n")
        path.chmod(0o755)

    def archive(self, version="v0.1.0", extra=None, symlink=False):
        path = self.downloads / self.asset
        with tarfile.open(path, "w:gz") as tar:
            content = f"#!/bin/sh\nprintf 'hrelay {version}\\n'\n".encode()
            if symlink:
                info = tarfile.TarInfo("hrelay")
                info.type = tarfile.SYMTYPE
                info.linkname = "/nonexistent/hrelay"
                tar.addfile(info)
            else:
                info = tarfile.TarInfo("hrelay")
                info.size = len(content)
                info.mode = 0o755
                tar.addfile(info, io.BytesIO(content))
            for name in ("LICENSE", "README.md", "THIRD_PARTY_NOTICES.txt"):
                info = tarfile.TarInfo(name)
                info.size = 4
                tar.addfile(info, io.BytesIO(b"test"))
            if extra:
                info = tarfile.TarInfo(extra)
                info.size = 4
                tar.addfile(info, io.BytesIO(b"test"))
        digest = hashlib.sha256(path.read_bytes()).hexdigest()
        (self.downloads / "checksums.txt").write_text(f"{digest}  {self.asset}\n")

    def install(self, success=True):
        result = subprocess.run(["/bin/sh", str(self.root / "scripts/install-binary.sh")],
                                env=self.env, capture_output=True, text=True, timeout=10)
        if success:
            self.assertEqual(result.returncode, 0, (result.stdout, result.stderr))
        else:
            self.assertNotEqual(result.returncode, 0, result.stdout)
        return result

    def old_binary(self):
        (self.root / "bin").mkdir()
        path = self.root / "bin/hrelay"
        path.write_text("keep existing binary")
        return path

    def test_pinned_download_and_atomic_install(self):
        old = self.old_binary()
        self.install()
        self.assertIn("hrelay v0.1.0", old.read_text())
        self.assertEqual(old.stat().st_mode & 0o777, 0o755)
        urls = (self.root / "urls").read_text().splitlines()
        self.assertEqual(len(urls), 2)
        self.assertTrue(all("/v0.1.0/" in url for url in urls))
        self.assertEqual(list((self.root / "bin").glob(".hrelay.*")), [])

    def test_platform_matrix(self):
        for system, arch, os_name, arch_name in (("Darwin", "x86_64", "darwin", "amd64"),
                                                ("Linux", "aarch64", "linux", "arm64"),
                                                ("Linux", "x86_64", "linux", "amd64")):
            self.env.update(TEST_OS=system, TEST_ARCH=arch)
            self.asset = f"hrelay_{os_name}_{arch_name}.tar.gz"
            self.archive()
            self.install()

    def test_checksum_mismatch_preserves_existing_binary(self):
        old = self.old_binary()
        (self.downloads / "checksums.txt").write_text(f"{'0' * 64}  {self.asset}\n")
        result = self.install(False)
        self.assertIn("SHA-256 mismatch", result.stderr)
        self.assertEqual(old.read_text(), "keep existing binary")

    def test_missing_or_duplicate_checksum(self):
        checksum = self.downloads / "checksums.txt"
        original = checksum.read_text()
        for body in ("", original + original):
            checksum.write_text(body)
            result = self.install(False)
            self.assertIn("missing or ambiguous", result.stderr)

    def test_download_failure_preserves_existing_binary(self):
        old = self.old_binary()
        self.env["TEST_DOWNLOAD_FAIL"] = "1"
        self.install(False)
        self.assertEqual(old.read_text(), "keep existing binary")

    def test_unsupported_platform_does_not_download(self):
        self.env["TEST_OS"] = "Windows"
        self.install(False)
        self.assertFalse((self.root / "urls").exists())
        self.env.update(TEST_OS="Linux", TEST_ARCH="riscv64")
        self.install(False)
        self.assertFalse((self.root / "urls").exists())

    def test_rejects_extra_paths_and_symlinks(self):
        for name in ("../escape", "hrelay"):
            self.archive(extra=name)
            self.install(False)
        self.archive(symlink=True)
        self.install(False)
        self.assertFalse((self.root / "escape").exists())

    def test_wrong_binary_version_preserves_existing_binary(self):
        old = self.old_binary()
        self.archive(version="v9.9.9")
        result = self.install(False)
        self.assertIn("version does not match", result.stderr)
        self.assertEqual(old.read_text(), "keep existing binary")

    def test_invalid_version_does_not_download(self):
        (self.root / "VERSION").write_text("../../latest\n")
        self.install(False)
        self.assertFalse((self.root / "urls").exists())


class ManifestTest(unittest.TestCase):
    def test_manifest_and_release_version_match(self):
        # 固定格式的契約檢查不依賴第三方 TOML parser，支援 Python 3.9。
        manifest = (ROOT / "herdr-plugin.toml").read_text()
        version = re.search(r'^version = "([^"]+)"$', manifest, re.MULTILINE).group(1)
        self.assertEqual("v" + version, (ROOT / "VERSION").read_text().strip())
        for line in ('id = "herdr-async-message-relay"', 'platforms = ["macos", "linux"]',
                     'command = ["sh", "scripts/install-binary.sh"]', 'command = ["bin/hrelay", "version"]'):
            self.assertIn(line, manifest)
        self.assertNotIn("[[startup]]", manifest)


if __name__ == "__main__":
    unittest.main()
