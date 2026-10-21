"""彙整二進位使用的 Go module 與 Go runtime 授權，隨 Release 成品散布。"""
import json
from pathlib import Path
import subprocess
import sys


def go(*args):
    return subprocess.check_output(["go", *args], text=True)


def main():
    used = set(go("list", "-deps", "-f", "{{if .Module}}{{.Module.Path}}{{end}}", "./cmd/hrelay").split())
    raw = go("list", "-m", "-json", "all")
    decoder = json.JSONDecoder()
    modules = []
    while raw.strip():
        item, end = decoder.raw_decode(raw.lstrip())
        raw = raw.lstrip()[end:]
        if item["Path"] in used and not item.get("Main"):
            modules.append(item)
    print("Third-party notices for hrelay\n")
    go_root = Path(go("env", "GOROOT").strip())
    # Homebrew 將 Go 的 LICENSE 放在 libexec 的上一層。
    runtime_license_root = go_root if (go_root / "LICENSE").is_file() else go_root.parent
    entries = [("Go runtime", runtime_license_root)]
    entries += [(m["Path"] + " " + m["Version"], Path(m["Dir"])) for m in sorted(modules, key=lambda m: m["Path"])]
    for label, root in entries:
        files = sorted({p for pattern in ("LICENSE*", "LICENCE*", "COPYING*", "NOTICE*")
                        for p in root.glob(pattern) if p.is_file()})
        if not files:
            raise RuntimeError("No license file found for " + label)
        for path in files:
            print("=" * 72 + "\n" + label + " / " + path.name + "\n" + "=" * 72)
            print(path.read_text())


if __name__ == "__main__":
    try:
        main()
    except Exception as exc:
        print(str(exc), file=sys.stderr)
        sys.exit(1)
