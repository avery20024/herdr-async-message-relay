# Caller-side sender verification (unreleased)

This source change is **not in released v0.8.2**. It runs in the sending CLI, before service ensure/registration and POST. It does not require a new database schema or worker API, and does not fix question-time delivery. Updating source files does not change an installed CLI or a running worker.

## Inputs and resolution

1. The CLI obtains its own `os.Getwd()`, resolves symlinks and verifies that it is an existing directory. It never asks the daemon for the daemon's working directory.
2. Use only the selected backend's bounded read-only adapter. Herdr reads `herdr agent list` and, when agents have `tab_id`, `herdr tab list`; effective addresses retain valid tab label > agent name > foreground basename precedence. Core reads `hc ls/get --json`, excluding remote/finished terminals, and uses core name > foreground basename, never App display labels. Only existing physical foreground directories strictly inside the selected root are eligible.
3. Match the caller's physical directory or its parents against eligible foreground directories, choosing the **most specific observed directory**, not a hard-coded language container or an arbitrary basename. A unique match can identify a sender without `HERDR_RELAY_FROM`.
4. For Herdr, `HERDR_PANE_ID` / `HERDR_TAB_ID` require a unique matching seat; if both exist, they must agree. For core, `HC_TERMINAL_ID` is the unique local terminal binding, scoped to the already-validated HC socket; inherited old Herdr pane/tab/context flags are ignored. A supplied but missing/duplicated seat is an error, not permission to fall back to the environment.
5. If pwd matches observed seats and a binding is present, that binding must be among the most-specific matches. A pwd/seat contradiction is rejected. Shared directories require a unique pane/tab binding: neither `--from` nor environment claims may arbitrarily select one of several seats.
6. From a repository or Worktree not matching a live foreground directory, a verified root-scoped pane/tab binding can identify the sender, including when the caller's Worktree is outside root. Never use its basename as an identity. If pwd does match another live foreground directory, that contradictory evidence is not ignored.
7. The selected complete address must resolve to exactly one valid live seat, regardless of its busy/idle state. Sending does not wait for either sender or recipient to become idle.
8. All nonempty declarations (`--from`, `HERDR_RELAY_FROM`, `HERDR_SEND_FROM`) must agree with the resolved identity. Conflicting values fail with diagnostics instead of silently overriding one another. This also protects a copied wrapper which supplies a stale environment value as `--from`.

## Offline/manual exception

Outside the selected backend's seat—no Herdr environment/pane/tab binding for Herdr, or no `HC_TERMINAL_ID` for explicitly selected core—an authorized explicit `--from ROLE` preserves manual/offline sending when no directory/seat can be resolved or lookup is unavailable. The core operator path requires explicit `HRELAY_BACKEND=core` and an absolute `HC_SOCKET`; auto rejects socket-only context. If live directory evidence exists it is still checked. Nonempty conflicting environment declarations still reject the request. **Environment alone never enables this exception.** Lookup failure inside a core terminal is not permission to use this exception. Do not unset context as a workaround; fix the launch/resume context instead.

This deliberately keeps the external operator path, not an authentication bypass claim. The local OS account remains the trust boundary. A process sharing that account can alter environment variables or directly use the socket; the check prevents accidental identity confusion, not malicious impersonation. Directory lookup and enqueue are not atomic.

## Examples

| Caller context | Result |
| --- | --- |
| Unique reviewer foreground directory; no sender hint | Resolve reviewer from live directory |
| Reviewer pwd; `HERDR_RELAY_FROM=gm` | Reject before ensure/POST |
| Reviewer pwd; inherited GM pane/tab | Reject pwd/seat conflict |
| Shared directory; named dispatch tab and matching pane | Resolve that complete dispatch address |
| Shared directory; only `--from dispatch-one` | Reject ambiguity |
| Worktree; verified reviewer pane and matching hint | Resolve reviewer, not Worktree basename |
| Herdr/core lookup failure inside a seat | Reject; do not start another service |
| No selected-backend seat context or live match; authorized explicit `--from operator` | Manual declaration allowed |
| Core terminal with inherited old Herdr pane/tab | Use the HC terminal binding, not old backend topology |
| Core shared directory with unique HC terminal ID | Resolve that terminal's complete role |
| Same complete address claimed by two agents | Reject, even if only one is idle |

Keep sender/tab labels stable. Do not auto-correct a conflict, blindly change cwd to evade it, restart the relay, resend messages, or edit the queue. Inspect the actual shell used by the agent's tools; the outer AI process environment alone does not prove the shell inherited the right identity.

## Compatibility and recovery

- Both `send ROLE MESSAGE` and `send --to ROLE --body MESSAGE` use the same check, with or without `--ensure=false`.
- A legacy wrapper can remain installed; its `--from` declaration cannot override live identity checks. A wrapper that refuses missing sender context itself still needs a correct environment or a deliberate future wrapper update.
- Existing launch environments with incorrect sender or stale pane/tab values will now fail rather than continue mislabeling messages. Correct or relaunch the authorized seat through the team's handover process; installing the new CLI does not rewrite running shells.
- Recipient addresses can remain offline and do not need individual directories. Sender verification may inspect backend topology (including other terminals) before enqueue, but does not wait for or require recipient availability/readiness. The enqueue API itself never invokes delivery; persistence still precedes worker submission.
- Body, recipient, key and success JSON remain unchanged. Existing accepted/submitted history is not relabeled, replayed or migrated. `submitted` is still not a read receipt.
