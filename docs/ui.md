# Read-only message history UI (development)

`hrelay ui` is a development feature, **not included in released v0.4.0 binaries**. Both the CLI and the running worker must support the new history API. Updating the executable does not upgrade an existing worker. Installation and service upgrades are separate operations; the UI never performs them.

```sh
hrelay --root /absolute/team ui
hrelay --lang zh-TW --root /absolute/team ui
hrelay help ui
```

## Layout and controls

The outer title shows the canonical team directory name; details show the full path. A response for another root is rejected. Six columns show sender, stored time, message state, a one-line body summary, recipient, and delivery time. History includes all four current states and is ordered by descending persistent sequence, not by a translated date string.

- All usable sizes have the same layout: the upper half is a full-width table; the lower half allocates 40% of its available width to metadata on the left and 60% to message body on the right (after excluding the frame and center separator; rounding goes to the body). The frame remains. Below 74 × 16 the UI asks for a larger terminal; quitting remains available.
- The first row is selected by default. `j`/`k` or arrows select rows. `u`/`d` move the table viewport and selection up/down by half its visible data-row count (minimum one), keeping the selected row visible; moving beyond the loaded tail fetches another page. Selection moves by the same number of rows so its screen position is retained except at history boundaries.
- `U`/`D` (Shift+u/d) scroll only the bottom-right body by one visible page; these keys do not change list selection or scroll metadata. Selecting a different message resets body scroll to the top. There is no pane focus switching or implicit key-target switching.
- The bottom-left pane contains all non-body message fields, root and timestamp/batch evidence. Long values wrap rather than ellipsize. `[` pages this metadata up and `]` pages it down by one visible page, independently of the list and body. Every wrapped metadata line is reachable even at 74 × 16; no data row is replaced with an overflow warning. Both lower headings display the first visible **line** and total lines, not a page count. Selecting a new message or applying different filters resets both offsets; refreshing the same ID preserves both, clamped when content/size changes. Compact help uses `^P`/`^W` to mean Ctrl+P/Ctrl+W and keeps all three navigation groups visible.
- `Ctrl+P` opens filters. Up/down chooses sender, recipient or state; left/right cycles a single value or All. `Enter` applies the combined AND filter; `Esc` discards the draft. Keys do not manipulate the table while the dialog is open. `Ctrl+C` always exits.
- Initial filters are All. This follows the confirmed all-history default; the exact prototype presentation remains adjustable.
- Refresh is manual by default: `r` refreshes. `Ctrl+W` toggles automatic refresh every second, shown in the bottom status line; pressing it again stops periodic refresh.
- Refresh preserves the selected message ID and its detail scroll, loading successive pages down to the selected sequence when necessary. If the message no longer matches the filters, selection moves to the first result with a notice. This fallback is explicitly documented for prototype evaluation.
- No overlapping request is started while a refresh/page load is running. A filter change cancels the old request; stale results cannot replace a newer filter. Each overall fetch has a ten-second deadline; each socket request has a five-second deadline.
- On failure, the last successfully loaded page remains visible with **DATA NOT UPDATED**. `r` retries; automatic mode keeps attempting once per second. If a fetch is already running, that tick is skipped.
- `q` (outside the filter dialog) or `Ctrl+C` exits the UI, restores the terminal and does not stop the relay. Main-screen `Esc` does not exit.

## Time and evidence

Times use Asia/Taipei (+08:00). The table uses `MM-DD HH:mm`; details include year, seconds and offset. Stored time is when the relay persisted the message, not when a recipient read it.

Only a submitted message whose associated batch has the ordinary `submitted` state uses the batch completion timestamp in the delivery-time column. This is an **automatic submission record**, not an end-to-end delivery/read receipt. A `resolved_submitted` timestamp is shown separately as a **manual confirmation record**, not the original submission time; its delivery-time cell remains `—`. Missing evidence is `—`. Batch detail is included as evidence, never as a command.

Bodies retain their stored values. Only the displayed representation strips ANSI/OSC, control codes and bidi override/isolate controls; summaries replace newlines with spaces and truncate using terminal cell widths. Details preserve visible text and line breaks with wrapping. The selected row has a `>` marker and reverse video, so selection does not rely on color alone.

## Read-only history API

The existing private Unix socket adds:

```text
GET /v1/messages?limit=50&before=123&sender=dev&recipient=gm&state=queued
```

- `limit`: default 50, range 1–100. The UI requests 50.
- `before`: optional positive exclusive sequence cursor; omit for the newest page.
- `sender`, `recipient`, `state`: optional exact AND filters. Empty means All. Unknown historical role values return an empty result, not a role-directory error.
- States: queued, dispatching, submitted, uncertain. Unknown/duplicate query parameters, invalid cursors and malformed query strings return HTTP 400.
- Response: canonical `project_root`, `items` (full message and nullable batch evidence), `next_before` (0 at the end), `senders` and `recipients` from all stored history. Nullable batch fields use the existing Go SQL null JSON shape (`String`/`Int64` plus `Valid`).
- Each page and its choices are read in one transaction; pages are not a cross-request database snapshot. A new insertion does not move an older sequence cursor. Filters may observe state transitions between pages; refresh re-evaluates them.
- SQL lives in `db/queries/queue.sql` with sqlc output checked in. There is no schema migration or change to dispatch/guard logic.

The UI uses only GET requests and rejects redirects, oversized responses, root mismatch, ignored filters and invalid sequence/cursor order. A worker without the endpoint shows an incompatibility error instead of falling back to the truncated `/v1/status` summary. It does not open a DB, start a service, enqueue, retry, resolve or delete messages.

## Limits and validation

History and filter choices are currently read from existing indexes; no new indexes are installed. Many distinct historical roles or repeatedly refreshing a very deep selection can be expensive. Requests have deadlines and failure retains the old view; large-scale performance is not yet benchmarked. UI rows accumulate only as history is paged; a refresh rebuilds through the selected message rather than preloading all history. Extremely large responses are rejected (24 MiB per page).

Bubble Tea is pinned to `charm.land/bubbletea/v2 v2.1.0`; terminal-width and terminal helpers are pinned through go.mod/go.sum. `scripts/notices.py` includes their licenses in release packaging automatically. No UI release/version bump is implied by this development change.

Tests cover SQL/API pagination beyond 100 rows, filtering, stale results, request cancellation, offline retention, timer generations, English/Traditional Chinese layouts, terminal sequence sanitization and time evidence. POSIX PTY tests build a temporary executable and use a fake private Unix-socket server, checking list half-page, body-page and metadata bracket keys independently, refresh/filter keys, resize, q/Ctrl+C, terminal mode restoration and alternate-screen exit. The fake server is stopped and child process groups are cleaned up. This does not replace native Linux/macOS independent review or a human visual acceptance session.
