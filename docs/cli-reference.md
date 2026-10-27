# CLI operations and advanced reference

Use [the UI](ui.md) for everyday history, filters and UI preferences. This reference covers service setup, sending, command-line inspection, recovery, configuration and development. UI preferences do not replace CLI language flags or worker configuration.

[README](../README.md) · [繁體中文](cli-reference.zh-TW.md)

## Current scope

This is an early release for macOS and Linux, arm64 and amd64. The Herdr compatibility baseline is **0.9.3**. Windows is not supported.

**Unreleased role discovery (not in published v0.6.0):** start with actual agents from `herdr agent list`; resolve each absolute `foreground_cwd` and the relay root through symlinks, require an existing directory strictly inside the root, and use its physical basename as the role. No fixed directory names, `cwd` fallback, or exact/subdirectory ranking remain. Root itself, missing/relative paths and symlink escapes are excluded. Duplicate role names remain ambiguous even when one seat is idle; invalid pane/sequence metadata is not delivered. Names start with a Unicode letter and continue with letters, marks, digits, `_` or `-`, up to 64 UTF-8 bytes; separators, whitespace and control characters are rejected. Keep the foreground directory stable: entering a child directory changes the role name. Senders are still explicitly declared, not authenticated; valid offline roles can be queued without pre-creating directories. Discovery does not synthesize Claude SessionStart/ready state.

The guard settings API lists discovered live roles plus persisted observation roles, not a hard-coded filesystem container. Discovery failures are exposed as `discovery_error`; the UI warns that observations may be incomplete, but an emergency policy disable remains available.

Starting with v0.2.0, `service start` / `send --ensure` can register and start a **launchd user agent on macOS** or a **systemd user service on Linux**. The Herdr startup hook restores registered, unpaused projects. Installation alone cannot safely infer a project root, so first use must supply `--root` or `DELIVER_ROOT`. No shell background fork, automatic role-directory creation, PATH modification, or harness skill injection is performed. The team supplies root and sender context; the caller chooses the target role. The short form `send ROLE MESSAGE` ensures the service automatically.

## Installation details

### As a Herdr plugin

Install without pinning a version:

```sh
herdr plugin install avery20024/herdr-async-message-relay
```

Omit `--ref` for normal installs to use Herdr's default source. Only specify it when you need a fixed version, rollback, or reproducible setup, for example:

```sh
herdr plugin install avery20024/herdr-async-message-relay --ref v0.5.0
```

Herdr updates follow the ref recorded at installation. If an existing installation is pinned to a tag, reinstall without `--ref` to switch to the default source; `herdr plugin update` alone keeps following that recorded tag.

The manifest's install-time `build` command downloads the matching **precompiled** executable; it does not run the Go compiler. It detects the OS/architecture, downloads the version pinned in `VERSION`, verifies its SHA-256 checksum and embedded version, and atomically installs `bin/hrelay`. The same native binary installs an independent executable copy at `$HOME/.local/bin/hrelay` (or absolute `HERDR_RELAY_BIN_DIR`), protected by an ownership hash and lock. It refuses to overwrite foreign or modified entries; no symlink to the managed checkout or shell shim is used.

Required installation tools: `sh`, `curl`, `tar`, `awk`, `mktemp`, and either `sha256sum` or `shasum`, plus ordinary Unix utilities. Herdr uses Git to obtain the plugin. No Go, Python, or sqlite3 CLI is needed to install or run the binary.

Herdr does not modify agent PATH. The stable command is immediately usable only if its directory is already selected by that agent's PATH; otherwise the installer reports `on_path: false`. Configure PATH before agent launch or use the reported absolute path; an export in another terminal does not update live processes. Actions expose **Install or repair hrelay command**, **Show relay version**, **Show relay services**, and **Start registered relays**. To locate the managed binary for repair, query `herdr plugin list --json` and the enabled entry's `plugin_root`; never guess checkout hashes. Existing wrappers remain optional compatibility clients.

### From a checkout

```sh
git clone https://github.com/avery20024/herdr-async-message-relay.git
cd herdr-async-message-relay
sh scripts/install-binary.sh
./bin/hrelay version
```

Only run `git checkout <tag>` before the installer when you need a particular version. Without a checkout override, the installer uses the cloned source's `VERSION`, not a separate GitHub latest-release lookup.

Alternatively, download the archive for your platform from [Releases](https://github.com/avery20024/herdr-async-message-relay/releases), verify `checksums.txt`, and extract it. Each archive contains `hrelay`, this README, the MIT license, and third-party license notices. Keep the notices with the executable when redistributing.

Checksum verification detects corruption or a mismatch against the downloaded manifest; it does not protect against a compromised publisher or compromised Release containing both replacement assets and checksums. Downloads require HTTPS and a reachable GitHub Release. There is no silent source-build fallback.

## Command overview (v0.4.0)

The overview is new in v0.4.0 and is **not included in v0.3.0 binaries**:

```sh
hrelay help
hrelay --help
hrelay --lang zh-TW help
hrelay help send
hrelay send --help
hrelay --lang zh-TW help service restart
hrelay cli install --help
```

Detailed topics cover every command and the `server`, `service`, and `cli` subcommands, including options, defaults, examples, and safety notes. `hrelay help COMMAND [SUBCOMMAND]` writes to stdout; `hrelay COMMAND [SUBCOMMAND] --help` (or `-h`) writes the same documentation to stderr and exits successfully. Unknown topics fail without loading configuration. All help works with an absent project, invalid worker settings, or no Herdr/service-manager tools, and never installs the CLI or touches a queue. Ordinary service inspection is different: `service status`/`list` may create service-manager bookkeeping.

Help recognition follows the normal flag parser: options precede positional arguments; `--help` used as a string option value or as message data is not intercepted. Global `--lang` / `--root` must still precede the command. For example, `send --to gm --body '--help'` remains a send request, not a help request. The short send form defaults `--ensure` to true; the `--to`/`--body` form defaults it to false. Detailed help documents this difference without starting a service.

It lists messaging, inspection, service controls, CLI installation, Herdr plugin actions, context settings, and safety reminders. `help` writes to stdout; root-level `--help` writes to stderr and also lists global flags. Neither requires a valid project, a running relay, or a Herdr socket, and neither starts services or creates a queue. Put global `--root` / `--lang` before the subcommand. The root is selected by `--root` → `DELIVER_ROOT` → current directory; hrelay does not search parent directories.

## Run

Use an existing team root with role directories, or create a test layout:

```sh
TEAM=/absolute/path/to/team
mkdir -p "$TEAM/agents/dev" "$TEAM/agents/gm"
```

### Automatic user service

Run the first setup from a Herdr pane so `HERDR_SOCKET_PATH` identifies the intended server, or explicitly set that variable to its absolute socket path. The user service must not guess which Herdr server owns a project.

```sh
# Manual wake-up without sending a message:
hrelay --root "$TEAM" server on

# Direct native command: optional flags precede the positional role and body.
hrelay --root "$TEAM" send --from dev --key example-report-1 gm 'Please inspect the report.'
```

On first use, the service snapshots the Herdr binary/socket context and worker settings, copies `hrelay` into durable private storage, and installs a per-project user service. Startup is bounded; a failure is reported before attempting to enqueue the new message. The positional `send ROLE MESSAGE` form ensures readiness by default. The original `send --to ROLE --body MESSAGE` flag form without `--ensure` retains the v0.1 manual-daemon behavior; `--ensure=false` explicitly selects manual mode for either form.

A healthy existing daemon is reused, not killed or upgraded. Its project is registered for later restoration, but it remains **unmanaged** until it exits and a later ensure/restore starts a managed replacement. Stop that foreground daemon normally before expecting the OS service to own it. The operator must preserve any custom `DELIVER_STATE_DIR` and worker environment during first registration.

```sh
hrelay --root "$TEAM" server status
hrelay --root "$TEAM" server off
hrelay --root "$TEAM" server on
hrelay --root "$TEAM" server restart
hrelay service list       # Registered projects for this Herdr context
hrelay service restore    # Restore enabled registrations only
```

`server on` is the friendly alias for `service start`; it starts an inactive relay or explicitly resumes a paused project, and does not restart a healthy daemon. `server off` aliases `service stop`, while `server status` aliases `service status`. These commands affect the explicitly configured **project**, not every project on the account. They do not need a sender role or a message body.

`server restart` (also `service restart`) holds one lifecycle lock across stop, shutdown confirmation, and start. It waits for both the listener to disappear and the daemon's file lock to be released; failed shutdown leaves the project paused and no replacement is started. A never-registered inactive project can be started by restart too, but an unmanaged legacy listener is never forcibly terminated. Inspect any interrupted dispatch uncertainty rather than blindly retrying messages.

`server off` / `service stop` persistently pauses the project and disables its login-time service. Neither `send --ensure` nor the startup hook will undo a pause; explicit `service start` is required. Stop refuses to terminate an unmanaged legacy daemon. A service-manager error remains an error: do not assume the process stopped just because the registry is paused.

Each project has a lifecycle lock in addition to the existing SQLite daemon lock. Separate projects remain separate services; a different nonempty Herdr socket context cannot take over the same registration. Herdr install/link/enable/update does **not** run startup hooks immediately: use first-send ensure or an explicit start/restore action without restarting Herdr.

The supervisor owns the daemon independently of a Herdr pane. The service can remain alive after Herdr exits, waiting for its socket to return. **Stop services before disabling or uninstalling the plugin**: Herdr has no uninstall hook to remove OS services automatically. No sudo or Linux linger configuration is applied. Linux requires a working systemd user session/DBus; macOS requires an available GUI launchd domain. Unsupported/headless setups should use the foreground mode below.

### Foreground mode

For manual supervision or troubleshooting:

```sh
./bin/hrelay --root "$TEAM" serve
```

Do not start it with an untracked shell `&`. Stop the foreground process with Ctrl-C. A single-instance lock prevents two workers from processing the same database. Plugin updates do not restart a healthy daemon automatically. For a managed upgrade or settings change, run `service stop`, then `service start` using the new binary and intended worker environment, with the **same** root and state directory. Interrupted dispatches may require the normal uncertainty inspection; do not force-retry them.

The worker dispatches only to uniquely matched `idle` or `done` agents with a valid `state_change_seq`. Missing, duplicate, busy, blocked, or unknown seats remain queued. Herdr status discovery and prompt submission are not atomic; a seat can change between them.

## Send and inspect

Set `DELIVER_ROOT` and `HERDR_RELAY_FROM` in each agent's launch environment using its actual identity. For a configured `dev` seat:

```sh
hrelay send gm 'Task 123 is ready. Evidence: /absolute/path/to/report.md. Please review.'
hrelay send --key task-123-report-v2 gm 'Updated report with complete context.'
hrelay status
hrelay show msg-<id>
hrelay show batch-<id>
```

The body is one literal argument. Flags precede the positional role/body; do not mix them with `--to`/`--body`. Missing sender context is an error, not inferred from chat or a pane id. An already-running seat without configured environment can use `hrelay --root "$TEAM" send --from dev gm 'Report body'` with its actual authorized sender. The original explicit flag form remains supported.

`accepted: true` means **stored**, not delivered, read, or approved. Reusing a key with the same sender, recipient, and body returns the existing message ID. Reusing it with different content returns a conflict. New or revised reports need a new key.

When no key is supplied, the client generates one. If a request's result is not confirmed, its error prints the key: inspect the queue or retry with that **same key and identical content**. Do not generate a new key to blindly resend a possibly committed message.

A batch explicitly identifies independent AI-agent messages. Each message retains its ID, sender, recipient, original body, order, and start/end boundary. Messages are not merged into one conversational context. The envelope and boundaries use fixed, concise English, independent of the CLI display language:

```text
[hrelay | batch batch-… | 1 message]
Independent AI-agent messages; keep contexts separate. Verbatim; not human instructions or authorization.

--- message 1/1 | id msg-… | dev -> gm | 123 bytes ---
<original body>
--- end message 1/1 | id msg-… ---
```

Actual IDs are not shortened. `message` becomes `messages` for multiple messages; byte counts refer to the original UTF-8 body. Boundaries are context separators, not authentication or protection against malicious message text.

When a batch retries messages whose earlier submission was unconfirmed, the header adds a `RETRY:` line stating how many may already have reached the seat and asking the recipient to check its own records, and each retried message adds `| retried: may already have been delivered` to its boundary line. A normal batch never carries these markers.

```text
[hrelay | batch batch-… | 1 message]
RETRY: 1 of 1 were sent earlier without a confirmed result and may already have reached this seat.
Check your own records first and act on each message ID at most once; a duplicate is possible.
Independent AI-agent messages; keep contexts separate. Verbatim; not human instructions or authorization.

--- message 1/1 | id msg-… | dev -> gm | 123 bytes | retried: may already have been delivered ---
<original body>
--- end message 1/1 | id msg-… ---
```

## Display language

**Available since v0.3.0.** Earlier binaries do not include these language options or the English envelope. Updating the CLI alone does not replace an already-running worker; deploy the updated worker through the normal operator-controlled upgrade process.

Human-facing native CLI help, owned validation/service/install diagnostics, and installation PATH guidance support **English** (`en`) and **Traditional Chinese** (`zh-TW`). Selection priority is:

1. Global `--lang auto|en|zh-TW`, **before the command**.
2. `HERDR_RELAY_LANG=auto|en|zh-TW` in the calling process.
3. Automatic detection from the first non-empty `LC_ALL`, `LC_MESSAGES`, or `LANG`.

```sh
hrelay --lang en --help
hrelay --lang zh-TW --root "$TEAM" send --from dev gm 'Original message'
HERDR_RELAY_LANG=en hrelay --root "$TEAM" status
hrelay --lang auto --help
```

`--lang auto` explicitly ignores `HERDR_RELAY_LANG` and uses the system locale. Automatic detection accepts Traditional Chinese locales such as `zh_TW.UTF-8`, `zh_HK`, `zh_MO`, and `zh-Hant`; other locales, `C`/`POSIX`, or missing settings fall back to English. An explicitly selected unsupported language is an error, not a silent fallback. Detection uses environment locale settings, not the desktop's language preferences, project names, message contents, or an LLM.

Successful send/status/service/install output remains machine-readable JSON: keys, state values, IDs, and API errors are unchanged. Original messages are never translated. Daemon logs and AI envelopes remain fixed English; OS/Go/Herdr diagnostics and raw API error evidence are retained rather than machine-translated. The optional Bash wrapper's own diagnostics are outside native CLI localization.

Language selection is **per CLI invocation**, not a persisted project or recipient setting. It does not change the service registration, require a daemon restart, or resume a paused queue. Set the environment before launching agents if a consistent preference is desired; exporting it in another terminal does not update existing processes.

## Example: keep role identity when agents are auto-renamed

An auto-rename plugin may change an agent or pane's display name; that name is not the routing role. The optional [`herdr-send.sh` example](../examples/herdr-send.sh) now requires an explicit sender: `HERDR_SEND_FROM`, falling back to `HERDR_RELAY_FROM`. It no longer scans a language-specific directory or infers identity from the caller's CWD. A previously copied wrapper is not upgraded automatically. Recipient discovery uses the foreground-directory rule above.

Copy the example into **your team's** `scripts/` directory, not the plugin's managed checkout:

```text
team/
├── scripts/
│   └── herdr-send.sh
└── agents/
    ├── dev/
    └── gm/
```

```sh
# From an existing source checkout; replace both absolute paths.
TEAM=/absolute/path/to/team
mkdir -p "$TEAM/scripts"
cp /absolute/path/to/herdr-async-message-relay/examples/herdr-send.sh "$TEAM/scripts/herdr-send.sh"
chmod +x "$TEAM/scripts/herdr-send.sh"

# The sender is explicit even if its display name has changed:
cd "$TEAM/agents/dev"
HERDR_RELAY_FROM=dev ../../scripts/herdr-send.sh gm 'Task 123 is ready. Evidence: /absolute/path/to/report.md. Please review.'

# From the team root, declare the actual sender explicitly:
cd "$TEAM"
HERDR_SEND_FROM=dev HERDR_SEND_IDEMPOTENCY_KEY=task-123-report-v1 \
  ./scripts/herdr-send.sh gm 'Complete report and requested next step.'

# Retry only with the same key, sender, recipient, and original body:
HERDR_SEND_FROM=dev HERDR_SEND_IDEMPOTENCY_KEY=task-123-report-v1 \
  ./scripts/herdr-send.sh gm 'Complete report and requested next step.'
```

The wrapper is equivalent to:

```sh
hrelay --root "$TEAM" send --from dev --key task-123-report-v1 gm 'Complete report and requested next step.'
```

Without `HERDR_SEND_IDEMPOTENCY_KEY`, the wrapper omits `--key`. Important boundaries:

- Root uses `DELIVER_ROOT` when set, otherwise the script's parent directory. Copy it to `<team-root>/scripts/herdr-send.sh` or provide the intended root explicitly.
- Sender uses `HERDR_SEND_FROM`, then `HERDR_RELAY_FROM`; missing identity is rejected. It passes `--from` explicitly and never guesses from the current path.
- Native CLI validation owns name safety; role directories need not be present or online at enqueue time. Unknown recipients remain queued until a matching seat appears.
- Renaming a display label does not change routing; changing the foreground directory basename does.
- Requires Bash and a compatible native `hrelay` on PATH. No jq, agent discovery in the wrapper, checkout fallback or legacy binary override is used. Wrapper diagnostics are English; this file documents unreleased wrapper behavior.
- The body remains one unchanged argument, including newlines; use proper shell quoting. The wrapper neither reads rename-plugin data nor injects keys or direct prompts.
- Short-form send ensures the same project service before enqueueing. First registration still needs the intended absolute `HERDR_SOCKET_PATH`; custom state/service paths must remain consistent. A persistent pause is not bypassed.
- `accepted: true` still means stored, not read or task acceptance. Preserve the returned ID/key and follow the uncertainty rules above rather than blindly resending.

For agents whose launcher already supplies `DELIVER_ROOT` and `HERDR_RELAY_FROM`, prefer `hrelay send ROLE MESSAGE` directly. Installing the plugin does not copy this optional script into a team or change its SOP.

## Configuration

Foreground settings are read at startup. Managed services snapshot settings at registration; normal send/restore does not overwrite them. To change batch/poll settings, stop the service, set the desired environment, and explicitly start it again. The registered Herdr context is not silently rebound.

| Variable | Default | Meaning |
|---|---|---|
| `DELIVER_BATCH_SIZE` | `3` | Maximum messages per batch, 1–100 |
| `DELIVER_BATCH_BYTES` | `12000` | Maximum rendered batch bytes, 2048–64000; individual body limit is this value minus 1024 |
| `DELIVER_POLL_MS` | `500` | Scan interval in milliseconds, 50–60000 |
| `DELIVER_RETRY_BACKOFF_MS` | `30000` | Wait before retrying an `uncertain` batch, 0–600000 |
| `DELIVER_MAX_ATTEMPTS` | `3` | Maximum deliveries per message before it is held for inspection, 1–10 |
| `DELIVER_ROOT` | Working directory | Team root; configure explicitly for agents; `--root` overrides it |
| `HERDR_RELAY_FROM` | None | Actual sender role supplied by the team launcher; `--from` overrides it |
| `HERDR_RELAY_LANG` | `auto` | Native CLI display language: `auto`, `en`, `zh-TW`; global `--lang` overrides it; not saved in service records |
| `HERDR_RELAY_BIN_DIR` | `$HOME/.local/bin` | Stable CLI installation directory; absolute path; not a project/service setting |
| `DELIVER_STATE_DIR` | Root-scoped directory below | Explicit state/socket location; clients and daemon must agree |
| `HERDR_BIN_PATH` | `herdr` | Herdr binary used for discovery and submission |

Example:

```sh
DELIVER_BATCH_SIZE=3 DELIVER_POLL_MS=500 ./bin/hrelay --root "$TEAM" serve
```

For backward compatibility with the original local prototype, the default state directory remains:

```text
${XDG_STATE_HOME:-$HOME/.local/state}/herdr-deliver/<canonical-team-root-hash>/
```

It holds `queue.db`, SQLite WAL/SHM, `deliver.sock`, and `daemon.lock`. Directory permissions are 0700; the database and socket are 0600. A database is bound to one canonical root and cannot be reused for a different project root. Multiple Herdr servers are not automatically isolated: use distinct state directories and consistent Herdr connection environments where needed.

Never put durable state inside a Herdr-managed plugin checkout. The service registry and content-addressed executable snapshots live separately under `${XDG_STATE_HOME:-$HOME/.local/state}/herdr-async-message-relay/services/`, overridable with an absolute `HERDR_RELAY_SERVICE_HOME` that must match for callers and startup hooks. This deliberately does not depend on `HERDR_PLUGIN_STATE_DIR`, which is absent in ordinary caller shells. The queue location above is unchanged; no automatic database migration occurs. A shorter explicit `DELIVER_STATE_DIR` is useful if the Unix socket path exceeds the platform limit.

The registry stores root, queue path, allowlisted environment, and supervision state in 0600 JSON files inside 0700 directories. Only selected Herdr/context and worker settings are persisted, not the caller's full environment. launchd definitions are under `~/Library/LaunchAgents/`; systemd definitions are under `${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/`. launchd logs go to the project's registry directory (`service.log`); Linux logs are in `journalctl --user -u <unit>`. Registry snapshots and macOS logs are not automatically pruned; arrange retention as needed. Service paths/environment containing control characters are rejected.

Do not shrink the batch byte limit below what an already queued message requires; its role will remain blocked until a sufficient limit is restored. The 500 ms interval is not a delivery-latency SLA: discovery, queue load, concurrency limits, and submission observation take additional time.

## Recovery and delivery semantics

| State | Meaning |
|---|---|
| `queued` | Committed and waiting for a ready role |
| `dispatching` | Fixed batch membership; an external submission may be in progress |
| `submitted` | Submission observation succeeded; not a read or processing receipt |
| `uncertain` | Submission may have happened; retried after a backoff, up to the attempt limit |

The worker uses bounded `herdr agent prompt --wait` observation, not a wait for the recipient's task to finish. A submission error or an interrupted dispatch conservatively produces `uncertain`; the same messages are then retried in FIFO order after `DELIVER_RETRY_BACKOFF_MS`, up to `DELIVER_MAX_ATTEMPTS` deliveries per message. Delivery is therefore **at least once**: a retry can duplicate a submission that had in fact reached the pane. Each message keeps its ID inside the batch envelope so the receiver can recognise a repeat. When a message reaches the attempt limit it is held for inspection and the remaining queue continues; a warning names the held message IDs. Other roles are never blocked by one role's uncertainty.

Retries are automatic; `resolve` is for inspected exceptions and for messages held at the attempt limit. After inspecting the recipient, prompt input, and task records, an authorized operator can resolve a batch:

```sh
# Confirmed not submitted: queue the same message IDs for another attempt.
./bin/hrelay --root "$TEAM" resolve \
  --batch batch-<id> --action retry --confirm \
  --note 'Inspected the target and records; this batch was not submitted.'

# Confirmed submitted: close transport uncertainty, not task acceptance.
./bin/hrelay --root "$TEAM" resolve \
  --batch batch-<id> --action submitted --confirm \
  --note 'Inspected the target and confirmed the original submission.'
```

Old batch membership is retained even after requeue. `--confirm` records explicit intent; it is not authentication.

SQLite and Herdr cannot share a transaction, so **exactly-once delivery is not guaranteed**. A submitted message is not automatically replayed if the receiving pane dies before processing it. The stored body and task records remain available for manual reconciliation. Reading a message does not acknowledge completion. There is no forced key injection or `pane send-text` fallback.

## Security and privacy

The trust boundary is the local OS account, not isolation between agents sharing it. Any process that can access the socket can declare a role and use the API; role labels are not authenticated identities. Agent messages do not grant human approval for protected actions.

Queue data contains full message bodies and potentially sensitive local paths. Protect state backups, do not commit them, and do not post `status`/`show` output publicly without inspection. Stop the daemon before copying its state directory, including SQLite sidecars. History is not automatically pruned or encrypted.

## Development

Requirements: Go 1.26+, Python 3.9+ for tests/packaging, and sqlc 1.31.1 when regenerating queries.

```sh
make build
make check
make generate
```

Local plugin linking does **not** execute install-time build commands:

```sh
make build
herdr plugin link "$PWD"
```

The source of application SQL is `db/queries`; generated code is under `internal/sqlc`. Tests use temporary SQLite databases, Unix sockets, fake Herdr commands, fake launchctl/systemctl runners, and fake downloaders. They do not notify live agents, invoke real OS service managers, or start TCP listeners. Native service-manager smoke testing is a separate explicitly controlled step.

## Releases

Binaries and archives are ignored by Git. No pre-push hook adds commits or modifies history.

1. Update `VERSION` and `herdr-plugin.toml` to matching versions.
2. Run `make check`, commit the source, and push it. Wait for CI to pass on Linux and macOS.
3. Push the matching `vX.Y.Z` tag at the tested commit.
4. The Release workflow checks the version, runs tests, cross-compiles four platforms with `CGO_ENABLED=0`, and publishes archives plus `checksums.txt` only after every platform succeeds.

To build an archive locally:

```sh
sh scripts/package.sh darwin arm64
```

Cross-compilation alone does not establish native runtime compatibility on every target; Linux/macOS native CI supplements it. General role registries, harness adapters, read receipts, and automatic retention are separate future work. The plugin never infers business roles. The launcher/SOP supplies project and sender context; the caller chooses the recipient. Team-specific wrappers can remain as compatibility clients, but native CLI use does not depend on them.
