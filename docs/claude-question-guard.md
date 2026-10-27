# Claude question observation and team interception

> **Current default:** a missing team policy defaults to **off**, preserving existing explicit on/off policies. Scoped Hooks still record events while off. Enabling interception still requires valid Hooks/session state; corrupt policy remains an error, not an opt-out. The UI label is **Claude interception (global)**, scoped to one relay root.
>
> **Historical behavior (v0.6.0).** v0.5.1 and earlier use per-role opt-in. v0.6.0 introduces a **team-wide, default-on** interception policy. Publishing a release does not deploy it; updating the CLI alone does not upgrade a running worker or install Hooks.

This is an additional defense for Claude Code `AskUserQuestion`, not a fix guarantee or a replacement for Herdr's blocked-state detection. Hooks installation remains explicit and scoped; the plugin never changes Claude settings or restarts agents automatically.

## Recording and interception are separate

- Every configured, valid Hook records lifecycle events, even when team interception is **off**. No prior per-role `enable` is needed.
- The **global interception** setting defaults to **off** when `team-policy.json` is absent. Ctrl+P in `hrelay ui` loads it from the worker. Select **Claude interception (global)** with up/down, change with left/right, and press Enter to save. Esc discards the draft. This setting affects the current canonical team root, not every team on the machine.
- When on, Claude delivery requires both Herdr idle/done and a matching ready Hook record. Missing initialization, pending questions, unknown/ended state, corrupt records, or session/pane mismatch leave messages queued. Other agent kinds do not use the Claude gate. Classification depends on Herdr reporting `agent_session.agent = claude`; the relay does not infer Claude identity from display names.
- When off, notification delivery uses Herdr-only gating. `blocked` still prevents delivery. The switch does not answer a question, clear pending state, or claim human approval.
- Switching off/on never resets observations. A question observed while off still blocks after re-enabling until its matching completion arrives. No timeout invents a completion.
- `PostToolUseFailure` marks the matching session/pane unknown even when its tool ID has no recorded start. The failure is persisted with an unknown start (zero) and no invented duration; unrelated pending questions are preserved. A same-session SessionStart or completion of another question cannot clear this unknown state.
- Guard state is **observed evidence, not a Hook-health heartbeat**. A previously ready record does not prove that Hooks remain installed or functioning.

The UI shows two muted hints, not observation counts or a Hook-health claim. The API still returns persisted observation statuses for discovered role names and existing observation directories, including non-Claude roles. Uninitialized means no usable lifecycle record; it cannot distinguish missing Hooks from a configured session that has not started. Use CLI role status for diagnostics; role discovery or successful submission never creates a ready observation.

### Consistency and failures

The setting is saved atomically under a team advisory lock. Worker submissions hold a shared team lock and the target role's lock until the result is recorded. Setting changes take the exclusive team lock, so an in-flight submission finishes before the switch is saved. Unrelated guarded roles can submit concurrently. Hooks take only their role lock, including when interception is off.

The worker reads the policy for every Claude dispatch, without restart. Concurrent explicit saves use last-writer-wins. An unchanged draft is not written, avoiding accidental resets by another UI instance. A save response lost to timeout is **unconfirmed**, not proof of failure: Ctrl+R reloads the actual policy before another edit. Older workers lacking the settings API show unavailable; the UI never writes a guessed default to them. Local language/theme saves are separate from team-policy saves; a partial save is reported.

Hooks remain synchronous. With interception on (or a policy read error), validation/storage/locking failures exit 2, which may reject or delay AskUserQuestion; they never answer it. When interception is explicitly off, valid event recording failures warn on stderr and return success, so disabling can bypass a broken recorder. Missing required launcher variables, invalid CLI context, and malformed input still fail; the outer launcher also converts launch failures to exit 2. Recording is therefore not guaranteed during failures, missing events, or killed handlers.

## Scoped setup and migration

**Do not install the example into global/user Claude settings.** It requires team, role and Herdr pane context. In an unrelated Claude session, missing environment variables can block tools. Merge with the intended role/session's existing settings; do not overwrite other Hooks.

1. Use the matching release binary and worker in an isolated test root/state directory first.
2. Set the same canonical team root and state directory for worker and Hooks. Role discovery requires no fixed container directory is required: use a stable foreground directory strictly inside that root and its resolved basename as the role. v0.6.0 and earlier retain the original routing restrictions. Use a verified absolute binary path, not an ambiguous PATH entry. Role discovery and interception switches do not create SessionStart observations.
3. Merge [`../examples/claude-question-guard.settings.json`](../examples/claude-question-guard.settings.json) into that Claude session's scoped configuration. Required inherited context:

```sh
export DELIVER_ROOT=/absolute/test-team
export DELIVER_STATE_DIR=/absolute/test-state
export HERDR_RELAY_FROM=gm
export HRELAY_GUARD_BIN=/absolute/path/to/<version>/hrelay
# HERDR_PANE_ID must be inherited from the actual Claude pane.
```

4. Start a genuinely **new** Claude session. Its SessionStart binds the actual session and pane. Do not synthesize a ready event for a production pane. Enabling on an existing session with no start record intentionally leaves notifications queued; a first question without a valid start can also fail its synchronous Hook.
5. Validate question start, pending queue, human completion, and delivery in isolation before a separately authorized production upgrade.

Keep Hooks synchronous: **do not set `async: true`**. Success emits no permission decision or answer. The launcher example converts missing variables, binary errors and nonzero handler results to exit 2. Killed/timed-out Hooks or Claude ignoring errors remain limitations. Do not promise protection for every race.

### Upgrade from per-role opt-in

This is a behavior change, not a drop-in production restart:

- Preserve `<state-dir>/claude-guard/<role>/state.json` and its pending/session evidence.
- Old `<role>/policy.json` files are retained but **no longer consulted**. A legacy role `enabled:false` does not override an explicit enabled team policy. Old role enable/disable CLI requests are rejected with an instruction to use `--team`, rather than silently changing every role.
- Absent team policy means off. Existing explicit `enabled:true` policies are preserved and still defer uninitialized Claude roles. Before enabling, install scoped Hooks and validate session binding, question blocking and recovery. v0.6.0 defaults missing policy to on; upgrading the CLI does not switch a running v0.6.0 worker to the current default.
- No deployment, settings installation, agent restart, or service switch is implicit in this change.
- Valid legacy state remains readable; completed legacy questions may have no start timestamp, so duration cannot be reconstructed. Corrupt state is not reset by toggling the switch.

## CLI operations

These commands do not start or upgrade a service:

```sh
hrelay --root /absolute/team claude-guard --team status
hrelay --root /absolute/team claude-guard --team disable
hrelay --root /absolute/team claude-guard --team enable
hrelay --root /absolute/team claude-guard --role gm status
```

Role status returns team policy and the stored state, not live seat identity or a read receipt. Missing/corrupt role state is an error. The settings API labels missing initialization separately from unknown, ended and read errors; the Ctrl+P dialog omits those counts.

## Storage and timestamp evidence

Private local files under `<state-dir>/claude-guard/`:

- `team-policy.json`: strict `{"version":1,"enabled":true}` (or false). Both fields are required; duplicate/unknown fields, unsupported versions, nulls and malformed JSON are rejected. Read errors never become an opt-out. A corrupt policy requires deliberate inspected repair; normal saves do not overwrite it.
- `team.lock`: shared dispatch/exclusive setting-change advisory lock.
- `<role>/state.json`: latest phase, session/pane, pending IDs/start times, update time, and up to **20 recent question summaries** for bounded diagnostics.
- `<role>/events.jsonl`: **persistent append-only lifecycle events**, including question start/completion/failure timestamps and available duration. The 20-summary limit does not truncate this event history. No automatic retention/rotation is implemented; size grows with observed events. Back up/rotate only during an explicitly coordinated stop of that role's event writers; do not delete live evidence to unlock delivery.
- `<role>/guard.lock`: serializes that role's event writes and guarded submissions.

Times are Unix milliseconds; render/report them in Asia/Taipei (+08:00). Start is the observed PreToolUse time, not necessarily when the dialog first became visible; end is the observed matching success/failure, not a guaranteed human answer time. Missing completion remains pending. A new session or SessionEnd marks outstanding summaries interrupted without fabricating an end time or elapsed duration. No question text, tool input, answer, or message body is stored.

History is fsynced before the state replacement, but the two files are not one transaction. A crash can leave a received event in the log without the matching latest state. Treat the log as observation evidence, not an automatic unlock command. There is no exactly-once audit guarantee; repeated Hook invocations can repeat events. A killed partial write may leave an incomplete final JSONL line; preserve it as diagnostic evidence rather than treating it as completed state.

Input is limited to 1 MiB, state to 64 KiB, and pending questions to 100. Large identifiers or many simultaneous questions can hit the state-size limit before the count limit. Identifiers are limited; regular files/no-follow opens, private file modes and atomic state/policy replacements are used. The local OS account remains the trust boundary; this is not authentication against another process under the same account. No DB migration, new dependency, external model call or TCP listener is added.

## Recovery and rollback

Inspect actual Claude UI, session/pane identity, state and event records first. For lost completion or unknown state, start a genuinely new session when safe. Repeated SessionStart for the **same** session/pane preserves pending/unknown/ended evidence; resume/compaction is not proof a question ended.

Ctrl+P off (or `--team disable`) restores Herdr-only notification gating while continuing observation. This bypasses extra protection, not safely resolving an open question. Do not casually toggle during an unanswered dialog. Keep evidence and scoped Hooks when diagnosing. Removing Hooks is a separately coordinated rollback, not a setting change. Before downgrading, inspect questions and the target version's policy: v0.6.0 respects explicit team settings but defaults missing policy to on, unlike the current default; v0.5.1 and earlier use per-role opt-in and ignore team policy. Preserve state and set the intended policy deliberately rather than assuming a downgrade keeps the same protection.

## Limits and historical validation

Only AskUserQuestion is covered. Permission approvals, ExitPlanMode, trust dialogs, MCP elicitation and other interactive surfaces still depend on Herdr. Cancelled tools may omit PostToolUseFailure, so notifications can remain deferred indefinitely. Missing Hooks, launch/timeout failures, and non-atomic Herdr snapshot/submit behavior are not solved by a switch.

A previous implementation was tested with real Claude Code v2.1.308 / Herdr 0.9.3 on 2026-10-23: pending question kept the notification queued; completion was observed at 17:46:40.239 +08:00 and the batch was created at 17:46:42.647 +08:00. Herdr itself correctly reported blocked, so idle misclassification was not reproduced. Claude declined the requested acknowledgement. That test is not native validation of this new team-policy/UI implementation. Cancellation, interruption, contention and supported OS behavior still require independent/native validation.

References: [Claude Hooks](https://code.claude.com/docs/en/hooks), [Herdr #2868](https://github.com/herdrdev/herdr/issues/2868).
