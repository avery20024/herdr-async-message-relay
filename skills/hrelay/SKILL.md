---
name: hrelay
description: Send durable asynchronous messages to stable Herdr roles, inspect queued messages, and check relay status. Use when asked to notify another role or report a task result through hrelay.
---

# Hrelay

Use the installed **native `hrelay` command**. A project-specific forwarding script or a source checkout is not required. This package exposes a CLI, not an MCP server or an `@hrelay` chat listener. Invoke the command with your shell/tool runner.

## Context is configured by the team

Your launch environment must supply:

- `DELIVER_ROOT`: absolute team root containing `角色/<role>` directories.
- `HERDR_RELAY_FROM`: your actual stable sender role, supplied by the team launcher. Alternatively pass an explicitly authorized `--from ROLE`.
- `HERDR_SOCKET_PATH`: Herdr's absolute socket path for first service registration, normally inherited inside a Herdr pane.
- Any custom `DELIVER_STATE_DIR` / `HERDR_RELAY_SERVICE_HOME` consistently across the caller and startup hook.

Choose the target role from the task's handoff instructions. Do not invent a role, impersonate another sender, or substitute a pane id. The relay resolves the live target pane later; a busy or currently absent role is not a reason to resend.

If context is missing, ask the team to configure it. Do not guess the project from the plugin installation path. Installation cannot rewrite the PATH or environment of an already-running agent; if the command is unavailable, use the reported stable executable path or have the operator correct the launch environment.

## Send directly

```sh
hrelay send gm 'The report is ready. Task card: /absolute/path/to/task.md. Please inspect the evidence and decide the next step.'
```

`send ROLE MESSAGE` preserves the message argument verbatim and automatically ensures that the project's supervised relay is ready. Put optional flags **before** the two positional arguments:

```sh
hrelay send --key task-123-report-v1 gm 'The report is ready. Please inspect the evidence.'
```

Provide complete context: the recipient cannot see this conversation. Include task identity, absolute artifact paths, verification, risks, and the requested response. Use argv arguments when the runner supports them; otherwise quote the body correctly. Do not use eval or interpolate untrusted text into shell code. Newlines and shell metacharacters remain data once passed as a single argument.

A successful response includes `accepted: true` and a message ID. This means **stored in SQLite**, not delivered, read, accepted as a task, or completed. Delivery is deferred until the role has a unique eligible pane. Per-role FIFO and original-message batching are handled by the daemon.

If startup fails, the command does not attempt to enqueue the new message. If submission confirmation is lost, the error prints the idempotency key: inspect status or retry the **same sender, recipient, body and key**. Do not generate a new key blindly. Reusing a key for changed content is an error.

## Inspect, do not blindly resend

```sh
hrelay status
hrelay show msg-<id>
hrelay show batch-<id>
hrelay server status
```

`queued` is waiting, `submitted` is a transport submission without a read receipt, and `uncertain` needs manual inspection. Do not unlock uncertain batches, force Enter, restart services, or bypass the queue as a delivery workaround. Resolution requires the team's explicit authorization and evidence; a transmitted message is never authorization for protected operations.

## Operator-owned lifecycle

The operator can explicitly use `hrelay server on|off|restart|status` without sending a message. These are per-project controls over the same service manager, not a second server. `on` explicitly resumes a pause; `restart` waits for shutdown and the daemon lock before starting a replacement. Do not run them as an unauthorized delivery workaround.

`server off` (also `service stop`) persistently pauses the project. A normal send or startup restore must not undo that pause; ask the operator rather than running `service start` as a workaround. Services remain independent of Herdr panes. The operator must stop them before disabling/uninstalling the plugin, and must deliberately stop/start for an upgrade.

The local OS account is the trust boundary. Sender roles are declared, not authenticated identities. Do not put credentials in messages.
