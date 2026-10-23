# Experimental Claude question guard

This is an **experimental, opt-in feature introduced in v0.4.0**, not a fix guarantee or a replacement for Herdr's blocked-state detection. It does not install hooks, change Claude settings, restart a relay, or notify an agent automatically. It is opt-in **per recipient role**.

## What it protects

Claude Code's synchronous `PreToolUse` hook for `AskUserQuestion` records a pending tool call before the question executes. The worker checks that record before claiming messages. A successful `PostToolUse` clears only the matching tool call. Multiple questions remain blocked until all matching completions arrive.

For a role whose policy directory has never been created, valid supported hooks are silent no-ops and create no guard state; delivery keeps its existing Herdr-only behavior. This permits shared hook settings for existing role directories without implicitly enabling every role. Once the policy directory exists, missing or invalid policy is an error, not an opt-out. Root, role, pane, and event validation still apply.

The worker requires both its normal Herdr idle/done gate and the hook gate. Hook state must match the exact Herdr pane and the Claude `agent_session` (`agent: claude`, `kind: id`, `value: session_id`). Missing session metadata, missing/corrupt state, pending questions, ended sessions, and unknown states defer delivery. No batch is claimed and messages stay queued. Other roles keep working.

A per-role advisory lock serializes cooperative hook writes with delivery. For guarded delivery, the worker holds it while rechecking live role/pane/session/status/state sequence, claiming, submitting, and recording the result. This reduces the read/submit gap for synchronous hooks: if the pre-hook wins, delivery defers; if submission wins, the pre-hook cannot finish and display its question until the lock is released. A hook lock wait exceeding two seconds returns exit 2 rather than silently allowing the tool. This may reject/delay a question during a slow submission; it does **not** answer it.

## Setup (manual, isolated evaluation first)

Use a v0.4.0-or-later binary (or a built development binary), not an installed v0.3.0 binary. Both worker and hooks must use this implementation and the **same canonical team root and state directory**. Replacing only the CLI does not upgrade an existing worker. Production service upgrades need separate authorization.

1. Prepare the existing `角色/<role>` directory and a separate test state directory.
2. Configure the hook example below in the **test Claude session's** settings, merging with existing hooks instead of overwriting them. **Do not put these hooks in global/user-level Claude settings.** Keep them scoped to the intended Herdr role's test session: missing required environment variables (including `HERDR_RELAY_FROM` or `HERDR_PANE_ID`) cause exit 2 and can block AskUserQuestion in unrelated or non-Herdr sessions. For an enabled role, PreToolUse before a valid SessionStart also blocks the question.
3. Explicitly enable the role before starting that session:

```sh
export DELIVER_ROOT=/absolute/test-team
export DELIVER_STATE_DIR=/absolute/test-state
export HERDR_RELAY_FROM=gm
export HRELAY_GUARD_BIN=/absolute/development-checkout/bin/hrelay
# HERDR_PANE_ID must be the real Claude pane inherited from Herdr.
"$HRELAY_GUARD_BIN" --root "$DELIVER_ROOT" claude-guard --role gm enable
```

4. Start a **new** test Claude session so SessionStart binds its session and pane. Enabling a live session without a start event intentionally leaves it blocked. Do not simulate a ready event for a production pane.
5. Run the prototype worker with the same root/state in an explicitly managed test environment; no production queue is needed for unit tests.

See [`../examples/claude-question-guard.settings.json`](../examples/claude-question-guard.settings.json). `HERDR_RELAY_FROM` is used here only because the hook executes inside the explicitly configured recipient Claude role. The worker never guesses this binding from a display name. Use a verified absolute binary path, not a PATH entry that might select a released binary lacking this feature.

Hooks must be synchronous: **do not set `async: true`**. Hook success emits nothing, grants no tool permission, and supplies no answer. Hook errors exit 2; on PreToolUse this blocks the call. The settings example executes parameter expansion and the binary inside a subshell, with a parent-shell `|| exit 2`. Missing/empty required variables, a missing/non-executable binary, and nonzero CLI exits therefore return 2 while preserving stdin and argument boundaries. This does not guarantee a blocking result if Claude kills the whole handler, times out, never invokes it, or ignores its failure; those lifecycle assumptions still require native validation. Claude versions that support `onFailure: "block"` can additionally enforce launcher/timeout failures, but that setting is not assumed in this prototype. Verify your actual Claude version's behavior before deployment.

## Inspect and recover

```sh
"$HRELAY_GUARD_BIN" --root "$DELIVER_ROOT" claude-guard --role gm status
```

This reports the policy, phase, session, pane, pending tool IDs and timestamp. No question text or answers are persisted. Read errors remain errors, not ready. Existing queue `status` output does not embed guard state; use the separate guard command. Worker deferral details use debug logging.

- `SessionStart` initializes a new binding as `ready`. For the same session/pane it preserves existing state, including unknown/pending/ended; resume or compaction is not proof a question ended.
- `PostToolUseFailure` marks the binding `unknown`, even if a later success arrives. It does not automatically unblock.
- `SessionEnd` marks the session `ended`. Missing end events still cannot match a new session ID. Main-session lifecycle is not cleared by subagent SessionEnd; subagent question tool IDs also contribute to pending state.
- Claude's docs explicitly note that cancelling a running tool does not necessarily fire PostToolUseFailure. If completion is missing, the pending record remains indefinitely; **no timeout changes it to ready**.
- For interrupted/ended/corrupt state: inspect the actual UI and lifecycle first. Start a genuinely new session when safe. If corruption prevents initialization, deliberate disable/enable resets to unknown, followed by a new session; do not use a reset to bypass an open question.
- Repeated `enable` preserves pending state. Re-enabling after `disable` invalidates any old ready state and requires SessionStart again.
- To deliberately opt out after assessing the risk:

```sh
"$HRELAY_GUARD_BIN" --root "$DELIVER_ROOT" claude-guard --role gm disable
```

Disabling restores **Herdr-only gating**, not a safe resolution of an outstanding question. Do not disable while a question is open. Remove the optional hooks only as part of a coordinated rollback. Deleting the entire role policy directory also removes opt-in protection and is not a recovery procedure.

## Storage and limits

Records live below `<state-dir>/claude-guard/<role>/`: `policy.json`, `state.json`, and `guard.lock`. Private directories/files, no-follow file opens, serialized read/modify/write, fsynced temporary records and atomic renames are used. Do not place these in the plugin checkout or commit them. The normal local-OS-account trust boundary applies; this is not authentication against another process running as the same account. Existing directory permissions are not automatically tightened, and role-directory symlinks are not a security boundary.

Policies use the exact schema `{"version":1,"enabled":true}` (or explicit `false`). Both fields are mandatory and non-null, version must be 1, and duplicate, unknown, or misspelled fields are rejected. Malformed/unsupported policies fail closed in hooks, worker acquisition, and status; `enable` does not silently repair them. Only an explicit valid `enabled:false` policy opts out.

Unreleased prototype policies without `version` are intentionally rejected, not silently migrated. After inspecting the actual session and ensuring no question is open, deliberate `disable` writes a valid version-1 opt-out policy; `enable` then invalidates old ready state, and a genuinely new SessionStart is required. Preserve evidence before repair and do not delete the policy directory to force delivery.

Hook input is limited to 1 MiB; only lifecycle identifiers are stored. Up to 100 pending questions are supported. No DB migration, new dependency, external model call, TCP listener, background process or service registry change is required.

## Known limitations / not yet validated

- A real Claude Code v2.1.308 / Herdr 0.9.3 test confirmed SessionStart identity binding, a pending AskUserQuestion, queued notification while the question remained open, matching completion after the human selected the second option, and visible notification delivery afterwards. Pending cleared at 17:46:40.239 +08:00 on 2026-10-23; the delivery batch was created at 17:46:42.647 +08:00. Herdr correctly reported blocked, so this did not reproduce idle misclassification. Claude declined the notification's requested acknowledgement, so the content-handling assertion did not pass. Cancellation, interruption and native contention still need validation. Unit tests cover additional cases with synthetic hook payloads, fake Herdr and temporary SQLite.
- No official atomic query-and-submit API is added. Safety relies on hooks actually being configured, synchronous, invoked and completed before the UI proceeds. Missing/disabled hooks, a killed/timed-out handler, an unlaunchable binary, or Claude ignoring a failed hook can defeat that assumption. A previously ready record is **not proof hooks remain healthy**.
- Only AskUserQuestion is covered. Permission approvals, ExitPlanMode, directory-trust dialogs, MCP elicitation and other interactive surfaces still rely on Herdr. This must not be marketed as protection for every blocked dialog.
- There is no automatic retroactive query of a running Claude CLI, no heartbeat proof, no hook repair, no role registry beyond existing CWD routing, and no exactly-once delivery guarantee.
- Missing state defers; lost hooks can leave notifications stuck. Human inspection is preferable to automatic unlocking.
- Advisory flock polling has no fairness guarantee. Contending hooks may hit their two-second deadline and reject/delay the question; slow Herdr operations inside the submission lock can contribute. Mutual exclusion is not a liveness guarantee.
- Do not poll `Notification`/`permission_prompt` as a substitute: it is a different lifecycle and can arrive after a delay.

References: [Claude Hooks reference](https://code.claude.com/docs/en/hooks), [Herdr #2868](https://github.com/herdrdev/herdr/issues/2868).
