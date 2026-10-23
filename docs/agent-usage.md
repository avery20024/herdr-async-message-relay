# Native agent command and SOP integration

## The intended interface

```sh
hrelay send gm 'Please inspect the report and decide the next step.'
```

This calls the installed plugin's native executable directly. There is no project forwarding script, checkout-relative binary lookup, `herdr agent prompt` call, or natural-language `@hrelay` listener in the caller. Existing wrappers are optional compatibility clients, not a prerequisite.

The recipient role is a caller-selected stable key, not a pane address. Current routing uses `<team-root>/角色/<role>`; a general role registry is not part of this release. The worker, not the calling agent, resolves the current pane and waits for eligibility.

## Built-in command overview (v0.4.0+)

Version v0.4.0 introduces `hrelay help`; older binaries do not contain this overview. Root-level `hrelay --help` also shows it, followed by global flags. Choose the display language before the command:

```sh
hrelay help
hrelay --lang zh-TW help
```

The overview explains native commands and Herdr plugin action IDs without requiring project configuration or contacting a service. It does not create queues or start workers. `help` writes to stdout; root-level `--help` writes to stderr.

Normal installation does not need a pinned tag:

```sh
herdr plugin install avery20024/herdr-async-message-relay
```

Use `--ref` only for a deliberately fixed revision. The installer still verifies the Release selected by the source checkout's `VERSION`; omitting `--ref` does not disable checksum or embedded-version checks.

## One-time CLI setup

From v0.2.0, the plugin's binary installer also installs a **regular native executable copy** at `$HOME/.local/bin/hrelay`. Herdr itself does not modify PATH. The copy survives managed-checkout cleanup and is updated by later installs. It is not a shell shim or a symlink back to the plugin directory.

If that directory is already on an agent's PATH, newly invoked `hrelay` commands work without restarting its seat. If absent or shadowed by an earlier command, installation reports `on_path: false`; configure PATH **before launching agents**, or use the reported absolute path. Exporting in a different terminal does not change a live seat's environment.

The optional absolute `HERDR_RELAY_BIN_DIR` selects another installation directory. You can also run the plugin action **Install or repair hrelay command**, or call its binary directly:

```sh
/actual/plugin/root/bin/hrelay cli install --bin-dir /absolute/directory/already/on/PATH
```

The installer uses an ownership hash and a lock; it refuses foreign executables, symlink entries, corrupt ownership records, and modified copies. It never overwrites another `hrelay` just because the name matches. Choose another directory or have the operator inspect the conflict. The private `.hrelay-install.json` and `.hrelay-install.lock` are part of the installation. A failed metadata write requires inspection before retrying; do not delete ownership information blindly. Because this copy lives outside the managed checkout, uninstalling the plugin does not delete the stable executable or its ownership record. Stop services first; any later removal of the owned command is an explicit operator cleanup, not deletion of the entire bin directory.

Verify the selected command:

```sh
command -v hrelay
hrelay version
```

Release v0.1.0 does not contain short send syntax or stable CLI installation. Install v0.2.0 or newer before using these examples; a v0.1.0 binary cannot substitute for the native short-command interface.

## Per-agent context belongs to the team launcher

For a `dev` seat, the launcher should supply this environment **when starting that seat**:

```sh
DELIVER_ROOT=/absolute/path/to/team
HERDR_RELAY_FROM=dev
```

Each seat gets its own actual sender role. These are configuration values, not a second transport script. Preserve Herdr's inherited `HERDR_SOCKET_PATH`; first registration requires an absolute path and fails rather than guessing another Herdr server. The team chooses the recipient role on each send.

Already-running agents without these variables can use explicit arguments without a wrapper:

```sh
hrelay --root /absolute/path/to/team send --from dev gm 'Please inspect the report.'
```

The example sender must be replaced with the actual authorized sender. The short form never infers a business role from chat text or from `HERDR_PANE_ID`. A human outside a role seat must explicitly identify an authorized sender and the correct Herdr socket; there is no automatic `human` mailbox.

If customized, keep `DELIVER_STATE_DIR` and `HERDR_RELAY_SERVICE_HOME` consistent for senders and startup hooks. Otherwise use defaults. Do not relocate a queue merely to make a new command work.

## SOP text to adopt after setup

> Send role notifications through `hrelay send <target-role> <message>` using the native command installed by the relay plugin. Use the configured project root and actual sender role; the handoff instructions determine the target role. Include task identity, absolute artifact paths, verification, risks, and the requested next step. `accepted` means durably queued, not read or accepted as a task. For ambiguous confirmation, keep the original idempotency key and inspect before retrying; never blindly create another message. Do not bypass a persistent service pause or resolve uncertainty without authorization.

Useful commands:

```sh
hrelay send --key task-123-report-v1 gm 'Task report with complete context.'
hrelay status
hrelay show msg-<id>
hrelay show batch-<id>
hrelay server status
```

Optional flags come before the positional role and body. The original flag form remains compatible:

```sh
hrelay --root /absolute/path/to/team send --ensure \
  --from dev --to gm --body 'Task report with complete context.'
```

`send ROLE MESSAGE` ensures readiness by default; an operator managing a manual foreground daemon can explicitly use `send --ensure=false ROLE MESSAGE`. A missing sender or invalid request fails before service registration.

## Display language (v0.3.0+)

Starting with v0.3.0, the launcher may also set `HERDR_RELAY_LANG=en` or `HERDR_RELAY_LANG=zh-TW` for human-facing native CLI diagnostics/help. A one-off global override precedes the command:

```sh
hrelay --lang en --root /absolute/path/to/team send --from dev gm 'Original report'
```

Priority: `--lang` → `HERDR_RELAY_LANG` → first non-empty `LC_ALL`, `LC_MESSAGES`, `LANG`. `auto` selects the locale path explicitly; unsupported automatic locales fall back to English, while unsupported explicit choices fail. This is per invocation, not a persisted worker setting; another terminal's exports do not change existing agents.

Keep send/status/service/install JSON unchanged. Original bodies are never translated, and the AI batch header, independence/authorization reminder, and message boundaries are always concise English. OS/Herdr/Go errors and raw API error evidence remain verbatim; an English preference does not translate a separately copied wrapper's own diagnostics. Do not restart a service merely to switch CLI language. Deploying the new envelope does require a worker running the updated binary, using the normal operator-controlled upgrade process.

## Optional agent skill

This repository includes [`skills/hrelay/SKILL.md`](../skills/hrelay/SKILL.md). Link that directory into the harness's supported skill directory if desired; install does not silently edit Claude, Pi, or other harness configurations. Because managed plugin roots can move, check/update such links after a plugin source/location change. The stable native command does not depend on the skill link or checkout path.

Unlike herdr-mail, this release has no MCP door, inbox/read receipt, or ask/reply protocol. It borrows the direct-command and skill pattern, not the pane-granular mailbox or typed-key delivery fallback. Its durable stable-role queue and conservative uncertainty handling remain unchanged.

## Manual server control

With `DELIVER_ROOT` configured, the operator can wake the relay without sending anything:

```sh
hrelay server on
hrelay server status
hrelay server off
hrelay server restart
```

Alternatively prefix the command with `hrelay --root /absolute/path/to/team`. Server operations need project/Herdr context but do not require `HERDR_RELAY_FROM`. First registration still requires the absolute `HERDR_SOCKET_PATH`.

- `on`: start if inactive, explicitly resume if paused; reuse a healthy listener without restarting it.
- `off`: persistently pause and stop a registered managed service, then confirm its listener and daemon file lock are released. A healthy unmanaged foreground daemon is not killed. An unregistered project is reported rather than inventing an ownership record.
- `restart`: stop and start under the same lifecycle lock, with bounded shutdown confirmation. Works for running or paused managed services and can initialize an inactive unregistered project. If shutdown fails, do not start another worker. Unmanaged listeners are refused.
- `status`: display the same readiness/registration information as `service status`; unregistered projects are reported as such.

The original `service start|stop|restart|status` commands remain supported. These are per-project controls, not a global kill switch. Restart may interrupt an in-flight dispatch, so inspect `uncertain` instead of replaying messages automatically.

## Lifecycle ownership

The first short send registers the explicit project and starts a launchd/systemd user service if needed. Startup restore handles only registered, unpaused projects. A healthy legacy daemon is reused, not replaced. An explicit `service stop` remains paused until an operator explicitly starts it again. Stop managed services before disabling/uninstalling the plugin; removing the plugin or stable CLI alone does not stop OS jobs.
