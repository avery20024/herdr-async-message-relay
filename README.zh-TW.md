# Herdr Async Message Relay

[English](README.md) | **繁體中文**

為 [Herdr](https://herdr.dev) agent 提供持久化非同步訊息，以及可查閱歷史訊息的終端 UI。

**先入列，準備好再投遞，讓各角色持續工作。**

`hrelay` 將訊息寫入 SQLite 後便返回，不等待接收角色。獨立 worker 待該角色可接收時再投遞，某個角色忙碌不會阻塞其他角色。

## 功能

- **持久、非阻塞的訊息傳遞**：SQLite transaction commit 後才回覆成功，略過忙碌角色並維持同角色 FIFO。
- **穩定角色路由**：使用 `dev → gm`，而非暫時的 pane ID；尚未投遞的訊息會跟隨角色的新座位。
- **有界批次與安全重試**：預設每批最多三則、保留原文，提供冪等 key 與保守的不確定性復原。
- **本機傳輸**：私有 Unix socket 與 SQLite，不需要 TCP listener 或外部資料庫。
- **互動歷史介面**：透過 `hrelay ui` 查閱、篩選、檢視及刷新訊息，並在偏好設定選擇語言與主題。

## 訊息歷史 UI

**v0.5.0 已提供**。日常查閱歷史與完整本文，不必反覆執行查詢指令：

![hrelay ui：上方訊息歷史，下方詳細資料與完整本文](docs/assets/hrelay-ui.png)

*由實際執行中的 `hrelay ui` 終端擷取內容渲染成圖；訊息內容、本機路徑及內部識別資訊已遮蔽，保留原有布局與配色。*

- `Ctrl+F` 依發送方、接收方及狀態篩選。
- 列表、詳細資料、完整本文可獨立瀏覽，支援手動或每秒自動刷新。
- `Ctrl+P` 儲存**英文／繁體中文**及主題：Terminal、Catppuccin Latte、Dracula、Rosé Pine、Vesper。
- `?` 查快捷鍵；窄終端自動調整布局。主畫面按 `q` 離開，不會停止 worker。

在 Herdr pane 選定團隊根目錄；尚未啟動 relay 時，先執行一次啟動：

```sh
TEAM=/absolute/path/to/team
hrelay --root "$TEAM" server on
hrelay --root "$TEAM" ui
```

已設定 `DELIVER_ROOT` 的環境可直接執行 `hrelay ui`。UI 對佇列是唯讀的：不會啟動或升級 worker、不發送訊息、不處置 uncertain 批次，也不修改投遞設定。CLI 與 worker 都須相容 v0.5.0；更新插件不會替換已運行的 worker。

完整操作見 [UI 鍵位與偏好設定（英文）](docs/ui.md)。需要腳本、JSON 查詢、首次服務設定、CLI 語系旗標或復原時，再參閱 [CLI 操作與進階參考](docs/cli-reference.zh-TW.md)。

## UI 主題

目前提供五種內建主題：

| 主題 | 外觀 |
| --- | --- |
| **Terminal**（預設） | 依終端的淺色／深色背景調整。 |
| **Catppuccin Latte** | 淺色背景，搭配紫色重點色。 |
| **Dracula** | 深色背景，搭配紫色重點色。 |
| **Rosé Pine** | 柔和深色調，搭配玫瑰／淡紫色重點色。 |
| **Vesper** | 近黑背景，搭配暖桃色重點色。 |

按 `Ctrl+P`，用 `↑`／`↓` 選擇 **Theme／主題**，再以 `←`／`→` 切換，按 `Enter` 儲存套用，或按 `Esc` 取消。主題僅影響 UI，不會修改終端的全域配色設定。

以下預覽以同一份已遮蔽的擷取資料，由 v0.5.0 UI 渲染；訊息本文、本機路徑與內部識別資訊均保持遮蔽。深色預覽採用搭配主題的終端底色，實際畫布可能隨終端設定而不同。

### Catppuccin Latte

![Catppuccin Latte UI 主題：淺色背景與紫色重點色](docs/assets/hrelay-ui-catppuccin-latte.png)

### Dracula

![Dracula UI 主題：深色背景與紫色重點色](docs/assets/hrelay-ui-dracula.png)

### Vesper

![Vesper UI 主題：近黑背景與暖桃色重點色](docs/assets/hrelay-ui-vesper.png)

## Installation

支援 **macOS／Linux 的 arm64、amd64**，需要 **Herdr 0.9.3 以上**。在 Herdr 安裝插件：

```sh
herdr plugin install avery20024/herdr-async-message-relay
hrelay version
```

安裝器下載對應的預編譯執行檔，核對 checksum 與版本，並將穩定指令安裝至 `~/.local/bin/hrelay`。請在**啟動 agent 前**將該目錄加入 PATH，或使用安裝結果提供的絕對路徑；不需要 Go、Python 或 sqlite3 CLI。

### 所需依賴

需要 Herdr、Git、`curl`、CA 憑證、`sh`、`tar`、`awk`、`mktemp`，以及 `sha256sum` 或 `shasum`。macOS 已提供一般 Unix 工具與 `shasum`；Debian／Ubuntu 可一次安裝下列套件。這些是安裝範例，不是插件自動執行的指令。

**macOS — 已安裝 [Homebrew](https://brew.sh)：**

```sh
brew install git curl herdr
```

**Linux — Debian／Ubuntu：**

```sh
sudo apt-get update && sudo apt-get install -y git curl ca-certificates tar gawk coreutils
curl -fsSL https://herdr.dev/install.sh | sh
```

其他發行版請安裝對應套件，並參照 [Herdr 官方安裝說明](https://herdr.dev/docs/install/)。受管理的背景服務另需 Linux 的 **systemd user session／DBus**，或 macOS 的 **GUI launchd domain**；無頭環境可採 [前景模式](docs/cli-reference.zh-TW.md#前景模式)。

**Windows — 以 WSL 準備 Linux 依賴，不代表原生 Windows 支援：**

此插件尚無原生 Windows 執行檔。若要準備 Linux 環境，請在系統管理員 PowerShell 執行：

```powershell
wsl --install -d Ubuntu
```

依提示重新啟動並完成 Ubuntu 首次帳號設定，再於 **Ubuntu 裡執行上述 Linux 指令**。Herdr、agents 與 relay 都須位於同一個 Linux 環境；原生 Windows Herdr 與 WSL session 不能視為同一個環境。這條 WSL 路徑尚未完成端到端驗證，詳見 [Microsoft WSL 說明](https://learn.microsoft.com/en-us/windows/wsl/install)。

### 其他安裝方式

- 固定版本或回退可用 `herdr plugin install avery20024/herdr-async-message-relay --ref v0.5.0`；一般安裝省略 `--ref`，後續更新沿用安裝時記錄的 ref。
- 從 [Releases](https://github.com/avery20024/herdr-async-message-relay/releases) 下載平台套件，核對 `checksums.txt` 後解壓，保留附帶授權與第三方聲明。
- 從 checkout 安裝、修復 PATH、升級行為與 checksum 信任邊界見 [安裝細節](docs/cli-reference.zh-TW.md#安裝細節)。

## Agent 整合

在各 agent 的啟動環境設定團隊根目錄與實際發送身分：

```sh
export DELIVER_ROOT=/absolute/path/to/team
export HERDR_RELAY_FROM=dev
hrelay send gm 'Task 123 已備妥，請查核證據。'
```

接收角色對應 `<team-root>/角色/<role>`，其中 `角色` 是固定的目錄名稱；既有角色目錄與 agent 回報的 CWD 須一致。設定與 SOP 整合見 [agent 文件（英文）](docs/agent-usage.md)、可選 [skill（英文）](skills/hrelay/SKILL.md) 及 [CLI 參考](docs/cli-reference.zh-TW.md)。

**入列成功不等於已讀。** `accepted: true` 代表已儲存，不是已投遞、已讀或已核准。保留 message ID／冪等 key，先檢查不確定提交再重試，不要換新 key 盲目重送，詳見 [復原與投遞語意](docs/cli-reference.zh-TW.md#復原與投遞語意)。

## 文件

- [UI 鍵位、主題、偏好設定及限制（英文）](docs/ui.md)
- [CLI 操作、查詢、服務、設定、復原及開發](docs/cli-reference.zh-TW.md)
- [Agent 設定與 SOP 整合（英文）](docs/agent-usage.md)
- [Claude 問答安全閘門](docs/claude-question-guard.md) — 明確啟用的實驗性功能，不保證排除所有競態

佇列歷史可能含完整訊息與敏感路徑，分享截圖或匯出前請先檢查遮蔽；安全邊界為本機 OS 帳號，詳見 [安全與隱私](docs/cli-reference.zh-TW.md#安全與隱私)。

## 授權

[MIT](LICENSE)。Release archive 附有 Go runtime 與連結依賴的 `THIRD_PARTY_NOTICES.txt`。
