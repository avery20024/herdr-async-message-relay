# Claude question observation and interception

> **v0.8.2 behavior change:** enabled interception now blocks only positively observed, matching pending `AskUserQuestion` calls. Missing/unusable observations do **not** add a delivery block. Released v0.8.1 retains the earlier ready-record requirement. Updating the CLI does not update a running worker or install Hooks.

This is an additional best-effort defense against Herdr reporting `idle` while focus is in a question's free-text option. It is not a guarantee of human approval, a replacement for Herdr status detection, or protection for every interactive surface.

## Delivery rules

The worker still requires a unique seat inside its root, valid pane/state sequence, and Herdr `idle` or `done`. Herdr `blocked`/`working` still keeps messages queued. This change only relaxes the **additional Claude observation gate**:

| Observation, when interception is enabled | Additional block |
| --- | --- |
| No state / no SessionStart / no observed PreToolUse | None |
| Missing or mismatching current session/pane | None |
| Unreadable/corrupt observation, unavailable role lock or launch record | None; diagnostic warning for errors |
| Matching observed PreToolUse without matching PostToolUse | Block |
| Multiple observed questions | Block until each has a matching PostToolUse |
| Unmatched PostToolUse | Does not clear any pending question |
| PostToolUseFailure | Records failure; does not complete an observed pending question |
| Unknown phase with no observed pending | None |
| Matching harness SessionEnd | Ends that harness's observation; no extra block |
| Old harness generation after a recorded restart | None; late old-generation Hooks are rejected |

A first PreToolUse creates pending state without requiring SessionStart. Merely changing focus to free text, typing, waiting, or repeatedly reporting idle does not complete a pending question. No timeout invents a completion. Unknown diagnostics alone do not block; a matching Post clears its pending ID even after a failure.

**Accepted trade-off:** missing Hooks, lost starts, unreadable state or failed observation writes can allow delivery during an unanswered question. A previously ready record is not a Hook-health heartbeat. Conversely, a recorded start with a lost completion can remain pending indefinitely, until a real matching completion or new recorded harness launch. This is explicitly fail-open for unavailable observation, not guaranteed interception.

Interception defaults **off** when team policy is absent and preserves existing explicit on/off policies. While off, Hooks continue recording but delivery uses Herdr-only gating. Switching policy never resets observed pending questions. Invalid policy is a configuration error, not missing question evidence: malformed policy, invalid role/path/context and malformed Hook input are still errors. Root/uniqueness/sender validation is not relaxed.

The v0.8.2 UI restores **Claude interception (global)** as the third Ctrl+P preference, with two muted hints when space permits. Up/down selects it, left/right changes the draft, Enter saves a changed policy, and Esc cancels. Opening the menu only reads policy; it does not enable interception. An unconfirmed write requires Ctrl+R reload before retrying. The switch applies to the current relay root. Released v0.8.1 keeps it hidden. Use the matching worker version: a new UI talking to an old worker still gets the old strict interception behavior. This document is not permission to change production policy.

## Record actual harness launches

Use the same-version binary as a launch wrapper in an **isolated, authorized** Claude seat:

```sh
hrelay --root /absolute/test-team claude-run --role reviewer \
  --pane "$HERDR_PANE_ID" -- claude --model YOUR_AUTHORIZED_MODEL
# A restart/resume must pass through the wrapper again:
hrelay --root /absolute/test-team claude-run --role reviewer \
  --pane "$HERDR_PANE_ID" -- claude --resume YOUR_SESSION_ID
```

`claude-run` resolves the executable, writes a private role `launch.json` containing pane, launch time (Unix milliseconds) and a random run ID, then uses native exec with inherited terminal/signals. It does not start a worker/service or background process. It runs the command you explicitly provide; do not use it to start a model without authorization. If validation/recording/exec fails, it reports failure rather than claiming the harness started; a marker records a launch attempt, not proof of successful startup or AI readiness.

The wrapper supplies canonical `DELIVER_ROOT`, `DELIVER_STATE_DIR`, `HERDR_RELAY_FROM`, `HERDR_PANE_ID`, its own `HRELAY_GUARD_BIN` path and `HRELAY_CLAUDE_RUN_ID` to the child. Hooks inherit that run ID. `state.json` and append-only events preserve run ID, run-start time and Hook-observation time, allowing timing evidence to be compared without storing question or answer text.

A fresh run ID invalidates the previous run's pending state even when pane **and Claude session ID are reused**. An old Hook cannot overwrite or complete a new run's questions. The random ID is authoritative; timestamps are diagnostic, so wall-clock rollback or multiple launches in one millisecond cannot unlock a current question. Repeated SessionStart/compact in the **same** run does not clear pending.

All restart paths must use the wrapper for this guarantee. A legacy/unwrapped seat can use session/pane-bound observations, but a same-session, same-pane restart cannot be reliably distinguished without a new launch marker. Do not infer restart from `working → idle`, first-seen time, or a tab rename. Herdr App mods APIs are not assumed to be available in Herdr TUI 0.9.3. Existing live harnesses are not wrapped or restarted automatically.

## Install scoped Hooks separately

Merge [the example](../examples/claude-question-guard.settings.json) into the intended session/project configuration; do not overwrite existing Hooks or install it globally. Keep handlers synchronous (`async: true` is not supported). Match `PreToolUse`, `PostToolUse` and `PostToolUseFailure` on `AskUserQuestion`; SessionStart/SessionEnd record lifecycle metadata. Do not return `permissionDecision` or a fabricated answer.

The example reads the environment supplied by `claude-run`. Legacy explicit launch environments can still provide the same root/state/role/pane and verified binary path without a run ID, with the restart limitation above. Missing required environment variables or invalid input can still cause exit 2; these are setup errors, not unavailable observation state. Valid Hook observation/storage errors warn and return success even when interception is enabled, so the recorder does not itself deny a question. A corrupt explicit team policy still reports an error.

This is not a retrospective detector: enabling after a question began cannot reconstruct a missed start. SessionStart is metadata, not evidence that a person finished an earlier question. Never synthesize production ready/Pre/Post events to make a test pass.

## CLI and storage

These commands inspect/change local guard policy without starting/upgrading a service:

```sh
hrelay --root /absolute/test-team claude-guard --team status
hrelay --root /absolute/test-team claude-guard --team enable
hrelay --root /absolute/test-team claude-guard --team disable
hrelay --root /absolute/test-team claude-guard --role reviewer status
```

Role status reports stored evidence, not live identity or delivery/read receipts; missing/corrupt state can still be a diagnostic command error even though it does not block delivery. The API continues to distinguish uninitialized, pending, unknown, ended and errors; those labels are not all delivery blocks.

Private files under `<state-dir>/claude-guard/`:

- `team-policy.json`: strict version 1 policy; absent means off, corrupt is an error. Legacy per-role policies remain unused.
- `team.lock`: shared dispatch/exclusive policy lock; failure is still a policy synchronization error.
- `<role>/launch.json`: current wrapper launch attempt (random ID, pane, timestamp).
- `<role>/state.json`: session/pane/run, observed pending IDs/start times, recent summaries (up to 20).
- `<role>/events.jsonl`: append-only lifecycle evidence; no automatic retention/rotation.
- `<role>/guard.lock`: serializes launch records, valid observations and submission checks. Even missing state normally acquires this lock before dispatch. If the observation lock cannot be acquired, the new fail-open policy proceeds without that additional protection and warns.

Question content, answers and command arguments are not written by the launch/Hook recorder. Regular-file/no-follow checks, atomic private-file replacement, input/state size bounds and root isolation remain. The OS account is the trust boundary, not authentication against other processes using that account.

History append and state replacement are not one transaction. A crash can preserve an event without the updated state; do not replay history automatically to unlock delivery. Failed completion persistence can leave a known question pending; failed start persistence can miss protection. Preserve evidence rather than deleting it to unlock a seat.

## Verification, recovery and compatibility

Test no Hooks/no SessionStart, first observed Pre, repeated idle in free text, independent completion IDs, failure/cancel, lost Post, same-session restart, old delayed Hooks and compaction. Verify both queue behavior and actual Hook events in a separately authorized real test seat; mocks and PTY tests do not prove native Claude interaction.

Only AskUserQuestion is covered. Permission prompts, ExitPlanMode, trust dialogs and MCP elicitation still depend on Herdr. Cancelled or denied tools may not produce PostToolUse/PostToolUseFailure; do not treat every failure/cancel as completion. Herdr discovery/submission is non-atomic, and a Hook can fail or time out. There is no exactly-once delivery or universal question-protection guarantee.

For known pending with lost completion, inspect the actual dialog and records; finish it normally or restart through the wrapper when safe and authorized. Do not reset state using elapsed time or toggle policy as if that resolved an unanswered question. Old malformed state is preserved for deliberate repair; it is not automatically overwritten to claim coverage.

Downgrading to v0.8.1 restores its strict ready-state requirement; v0.6.0 additionally defaults missing policy to on. Preserve policy/state/history and review the target version's rules. No upgrade/downgrade, Hook installation, AI restart or production service switching is implicit.

Historical test on 2026-10-23 used Claude Code v2.1.308 / Herdr 0.9.3 and saw blocked throughout the question; it did not reproduce free-text focus reporting idle. New run-gating behavior requires new isolated/native validation. Published release notes remain historical.

Reference: [Claude Hooks](https://code.claude.com/docs/en/hooks).
