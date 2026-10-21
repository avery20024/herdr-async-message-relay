# Herdr Async Message Relay

Durable async messaging for [Herdr](https://herdr.dev) agents, with stable role routing and batched delivery.

**Queue now. Deliver when ready. Keep agents moving.**

`hrelay` accepts a message into SQLite and returns without waiting for its recipient to become idle. A separate worker discovers the recipient's current pane and delivers queued messages when that role is ready. Busy recipients do not hold up senders or other roles.

## Features

- **Durable acceptance:** respond only after the SQLite transaction commits; WAL and synchronous FULL.
- **Role addresses:** store `dev → gm`, not a temporary pane ID. Undelivered messages follow the role to its replacement pane.
- **Bounded batching:** up to **3 messages** per batch by default, configurable with `DELIVER_BATCH_SIZE`. Preserve original bodies and independent context boundaries; no LLM summarization.
- **Nonblocking dispatch:** scan every **500 ms** by default, skip busy roles, maintain per-role FIFO, and allow up to four recipients in flight.
- **Idempotent enqueue:** retry the same body and key without creating another message.
- **Conservative recovery:** uncertain submissions stop that role's queue until explicitly inspected and resolved.
- **Local transport:** private Unix socket and SQLite; no TCP listener or external database.

## Current scope

This is an early release for macOS and Linux, arm64 and amd64. The Herdr compatibility baseline is **0.9.3**. Windows is not supported.

Role discovery currently follows the convention `<team-root>/角色/<role>` (`角色` means “roles”). A role's live agent must report that exact CWD, or its resolved physical path, in `herdr agent list`. Role names are lowercase letters, digits, and hyphens, starting with a letter. A configurable routing registry is not implemented yet.

The plugin package currently handles **installation and version inspection only**. It deliberately does not start a daemon, install a service manager, inject harness skills, or configure role directories automatically. Daemon lifecycle integration is a separate design step.

## Install without Go

### As a Herdr plugin

Once the matching Release is available:

```sh
herdr plugin install avery20024/herdr-async-message-relay --ref v0.1.0
```

The manifest's install-time `build` command downloads the matching **precompiled** executable; it does not run the Go compiler. It detects the OS/architecture, downloads the version pinned in `VERSION`, verifies its SHA-256 checksum and embedded version, and atomically installs `bin/hrelay`.

Required installation tools: `sh`, `curl`, `tar`, `awk`, `mktemp`, and either `sha256sum` or `shasum`, plus ordinary Unix utilities. Herdr uses Git to obtain the plugin. No Go, Python, or sqlite3 CLI is needed to install or run the binary.

Herdr does not automatically put plugin executables on PATH. Locate the installation using `herdr plugin list` and its `plugin_root` field; do not assume a managed checkout path. The manifest exposes a **Show relay version** action. The executable may also be called directly by its full path.

### From a checkout

```sh
git clone https://github.com/avery20024/herdr-async-message-relay.git
cd herdr-async-message-relay
git checkout v0.1.0
sh scripts/install-binary.sh
./bin/hrelay version
```

Alternatively, download the archive for your platform from [Releases](https://github.com/avery20024/herdr-async-message-relay/releases), verify `checksums.txt`, and extract it. Each archive contains `hrelay`, this README, the MIT license, and third-party license notices. Keep the notices with the executable when redistributing.

Checksum verification detects corruption or a mismatch against the downloaded manifest; it does not protect against a compromised publisher or compromised Release containing both replacement assets and checksums. Downloads require HTTPS and a reachable GitHub Release. There is no silent source-build fallback.

## Run

Use an existing team root with role directories, or create a test layout:

```sh
TEAM=/absolute/path/to/team
mkdir -p "$TEAM/角色/dev" "$TEAM/角色/gm"
```

Start the daemon **in the foreground**, or supervise the same command with launchd/systemd:

```sh
./bin/hrelay --root "$TEAM" serve
```

Do not start it with an untracked shell `&`. Stop the foreground process with Ctrl-C or let the service manager send SIGTERM. A single-instance lock prevents two workers from processing the same database. Updates do not restart an existing daemon automatically; stop the old supervised instance and start the new binary using the **same** root and state directory.

The worker dispatches only to uniquely matched `idle` or `done` agents with a valid `state_change_seq`. Missing, duplicate, busy, blocked, or unknown seats remain queued. Herdr status discovery and prompt submission are not atomic; a seat can change between them.

## Send and inspect

In a second terminal:

```sh
./bin/hrelay --root "$TEAM" send \
  --from dev --to gm \
  --key task-123-report-v1 \
  --body 'Task 123 is ready. Evidence: /absolute/path/to/report.md. Please review.'

./bin/hrelay --root "$TEAM" status
./bin/hrelay --root "$TEAM" show msg-<id>
./bin/hrelay --root "$TEAM" show batch-<id>
```

`accepted: true` means **stored**, not delivered, read, or approved. Reusing a key with the same sender, recipient, and body returns the existing message ID. Reusing it with different content returns a conflict. New or revised reports need a new key.

When no key is supplied, the client generates one. If a request's result is not confirmed, its error prints the key: inspect the queue or retry with that **same key and identical content**. Do not generate a new key to blindly resend a possibly committed message.

A batch explicitly announces that it contains accumulated independent messages. Each message retains its ID, sender, recipient, original body, order, and start/end boundary. Messages are not merged into one conversational context. The envelope currently uses Traditional Chinese; configurable localization is not implemented.

## Configuration

Read at process startup. Restart the daemon to change worker settings.

| Variable | Default | Meaning |
|---|---|---|
| `DELIVER_BATCH_SIZE` | `3` | Maximum messages per batch, 1–100 |
| `DELIVER_BATCH_BYTES` | `12000` | Maximum rendered batch bytes, 2048–64000; individual body limit is this value minus 1024 |
| `DELIVER_POLL_MS` | `500` | Scan interval in milliseconds, 50–60000 |
| `DELIVER_ROOT` | Working directory | Team root; `--root` overrides it |
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

Never put durable state inside a Herdr-managed plugin checkout. Explicit `DELIVER_STATE_DIR` may point into `HERDR_PLUGIN_STATE_DIR` when future lifecycle integration supplies a consistent location to both daemon and clients; this package does not perform that migration automatically. A shorter explicit path is also useful if the Unix socket path exceeds the platform limit.

Do not shrink the batch byte limit below what an already queued message requires; its role will remain blocked until a sufficient limit is restored. The 500 ms interval is not a delivery-latency SLA: discovery, queue load, concurrency limits, and submission observation take additional time.

## Recovery and delivery semantics

| State | Meaning |
|---|---|
| `queued` | Committed and waiting for a ready role |
| `dispatching` | Fixed batch membership; an external submission may be in progress |
| `submitted` | Submission observation succeeded; not a read or processing receipt |
| `uncertain` | Submission may have happened; no automatic replay |

The worker uses bounded `herdr agent prompt --wait` observation, not a wait for the recipient's task to finish. A submission error or a daemon restart during dispatch conservatively produces `uncertain`, blocking later batches for that role. Other roles can continue.

After inspecting the recipient, prompt input, and task records, an authorized operator can resolve a batch:

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

The source of application SQL is `db/queries`; generated code is under `internal/sqlc`. Tests use temporary SQLite databases, Unix sockets, fake Herdr commands, and fake downloaders. They do not notify live agents or start TCP listeners.

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

Cross-compilation alone does not establish native runtime compatibility on every target; Linux/macOS native CI supplements it. Plugin daemon supervision, general role registries, harness adapters, read receipts, automatic retention, and marketplace listing are separate future work.

## License

[MIT](LICENSE). Release archives include `THIRD_PARTY_NOTICES.txt` for the Go runtime and linked dependencies.
