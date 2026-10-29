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

已設定 `DELIVER_ROOT` 的環境可在任何目錄直接執行 `hrelay ui`，詳見[團隊根目錄與實例](#團隊根目錄與實例)。UI 不會啟動或升級 worker、不發送訊息，也不處置 uncertain 批次。**預設：**UI 預設每秒自動刷新，缺少 Claude 攔截政策時預設**關閉**，保留既有明確設定。Ctrl+P 使用本 root 共用的「全域」標籤與兩行淡化提示；`?` 分為瀏覧、篩選、設置、其他。已接線的 Hooks 在關閉攔截時仍持續記錄問答時間。現行路由與缺省政策需重啟為對應版本的 worker；發布或更新 CLI／UI 不會替換運行中的 worker。較早版本（例如 v0.6.0）缺少政策時預設開啟，升級保留既有明確開啟／關閉設定；攔截開啟時，未初始化的 Claude 角色仍會延後通知。接線、舊政策遷移及限制見 [問答閘門文件](docs/claude-question-guard.md)。CLI 與 worker 都須相容 v0.5.0；更新插件不會替換已運行的 worker。

各版本變更、路由風險及升級注意事項見 [發布說明目錄](docs/releases/)。v0.6.0 加入依滑鼠位置分區捲動、左下淡色說明置底、欄位值強調，以及 `gg`／`G` 跳至已載入首尾資料。變更與升級注意事項見 [v0.6.0 發布說明](docs/releases/v0.6.0.md)，完整操作見 [UI 鍵位與偏好設定（英文）](docs/ui.md)。需要腳本、JSON 查詢、首次服務設定、CLI 語系旗標或復原時，再參閱 [CLI 操作與進階參考](docs/cli-reference.zh-TW.md)。

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

- 固定版本或回退可用 `herdr plugin install avery20024/herdr-async-message-relay --ref <tag>`；一般安裝省略 `--ref`，後續更新沿用安裝時記錄的 ref。
- 從 [Releases](https://github.com/avery20024/herdr-async-message-relay/releases) 下載平台套件，核對 `checksums.txt` 後解壓，保留附帶授權與第三方聲明。
- 從 checkout 安裝、修復 PATH、升級行為與 checksum 信任邊界見 [安裝細節](docs/cli-reference.zh-TW.md#安裝細節)。

## 團隊根目錄與實例

一個團隊根目錄對應一個 relay 實例。根目錄決定指令連到哪個佇列、socket 與背景服務，因此每個指令（包含 `hrelay ui`）都必須解析到與 worker 相同的根目錄。

告知 hrelay 根目錄有兩種方式：設定 `DELIVER_ROOT` 變數，或在根目錄放置 `.hrelay` 標記檔，讓其下任何目錄執行的指令由外層攔截到該根目錄。變數永遠優先於標記檔。

**根目錄的選擇順序**（先符合者優先）：

1. `--root PATH`（命令列顯式覆寫）
2. `DELIVER_ROOT`（變數，優先於標記檔）
3. 自當前目錄起向上最近、含有 `hrelay init` 所建立 `.hrelay` 標記檔的目錄
4. 以上皆無則報錯；hrelay 不會單以當前目錄作為根目錄

**從根目錄到實例：**根目錄會轉為絕對路徑並解開符號連結。狀態目錄為 `$XDG_STATE_HOME/herdr-deliver/<sha256(根目錄) 前 16 位十六進位>`（未設定時為 `~/.local/state`），內含佇列與 `deliver.sock`；可用 `DELIVER_STATE_DIR` 覆寫。受管理的 launchd／systemd 服務以根目錄與狀態目錄的雜湊識別。因此不同根目錄擁有各自獨立的佇列、socket 與服務，同一台機器可並行多個專案；而解析後指向同一目錄的不同路徑則共用同一個實例。若以不同根目錄（例如某個角色子目錄）存取同一專案，會連到另一個空的實例，角色探索也找不到座位。

**常見設定**

- AI 團隊：由啟動器在各座位環境設定 `DELIVER_ROOT` 與 `HERDR_RELAY_FROM`，座位可留在各自目錄，仍能連到團隊 relay。已設定 `DELIVER_ROOT` 時，`hrelay ui` 與 `hrelay send` 可在任何目錄執行。
- 個人專案：在專案根目錄執行一次 `hrelay init`，它會建立標記檔並印出環境變數的 `export` 範例；之後在該根目錄之下任何目錄執行 `hrelay server on`、`hrelay ui`、`hrelay send` 都不必再額外設定。
- 核對：`hrelay status` 會印出 `project_root`；發送前先確認它是預期的目錄。

`.hrelay` 標記檔只記錄格式版本、不記路徑，複製到他處不會指向原根目錄。只有同名的一般檔案有效，目錄與符號連結皆忽略，也不以目錄名稱推測根目錄。

## Agent 整合

在各 agent 的啟動環境設定團隊根目錄與實際發送身分：

```sh
export DELIVER_ROOT=/absolute/path/to/team
export HERDR_RELAY_FROM=dev
hrelay send gm 'Task 123 已備妥，請查核證據。'
```

**角色探索：**worker 先取得 `herdr agent list`，解析每個 AI 的絕對 `foreground_cwd` 與符號連結，實體目錄必須嚴格位於本 relay root 內，再取 basename 作角色名；中間目錄名稱與語言不限，例如 `agents/reviewer` 或 `開発/担当A`。不退回 `cwd` 或按舊角色目錄排名；路徑缺失、無效或同名衝突時不投遞。有效名稱可在座位離線時先入列，名稱以 Unicode 字母開頭，後續允許字母、組合符號、數字、底線與連字號，最多 64 UTF-8 bytes。請保持前景目錄穩定：進入子目錄會改變推導的角色。任意深度的同名目錄都參與定址；重名座位會使訊息維持 queued，若只剩另一個同名座位，它可接收該角色訊息。請使用可信任 root、避免無關同名座位。名稱精確比對，未做 Unicode 正規化或同形字辨識。此路由不適用於 v0.6.0 及更早版本，也不會自動初始化 Claude 問答觀測。設定與 SOP 整合見 [agent 文件（英文）](docs/agent-usage.md)、可選 [skill（英文）](skills/hrelay/SKILL.md) 及 [CLI 參考](docs/cli-reference.zh-TW.md)。

**入列成功不等於已讀。** `accepted: true` 代表已儲存，不是已投遞、已讀或已核准。保留 message ID／冪等 key，不要換新 key 盲目重送。若提交結果無法觀測，worker 會在短暫退避後依 FIFO 重送同一批訊息，最多 `DELIVER_MAX_ATTEMPTS` 次，因此投遞語意為 **at-least-once**；信封保留每則 message ID 供接收端辨識重複，詳見 [復原與投遞語意](docs/cli-reference.zh-TW.md#復原與投遞語意)。

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

## 文件

- [UI 鍵位、主題、偏好設定及限制（英文）](docs/ui.md)
- [CLI 操作、查詢、服務、設定、復原及開發](docs/cli-reference.zh-TW.md)
- [Agent 設定與 SOP 整合（英文）](docs/agent-usage.md)
- [Claude 問答安全閘門](docs/claude-question-guard.md) — 缺省攔截關閉（v0.6.0 為開啟），Hooks 仍需明確接線；不保證排除所有競態

佇列歷史可能含完整訊息與敏感路徑，分享截圖或匯出前請先檢查遮蔽；安全邊界為本機 OS 帳號，詳見 [安全與隱私](docs/cli-reference.zh-TW.md#安全與隱私)。

## 授權

[MIT](LICENSE)。Release archive 附有 Go runtime 與連結依賴的 `THIRD_PARTY_NOTICES.txt`。
