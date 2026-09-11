# Claude CLI idle replies

The Claude plugin bundles a command hook using `asyncRewake`. No channel launch
flag is required for this route. Tested with Claude Code 2.1.268; older clients
must support that hook field. Normal host hook permissions still apply.
Installation does not override workspace trust or disabled hooks.

The SessionStart hook supplies `hook_host` and `hook_session_id`. Pass these exact
values to `tincan_connect`, including when resuming an existing connection with
its saved handle. Connections cannot be rebound to another session. A plugin
refresh/new session may be needed to load updated hooks.

## Delivery

- SessionStart, PostToolUse and Stop start `tincan claude-wake` in the background.
  An OS lock permits only one waiter per session. Subagent hooks do not listen.
- The existing MCP process is the sole SSE/inbox consumer. The waiter checks its
  atomic local inbox snapshots once per second; it makes no network or model
  calls and does not hold the foreground turn open.
- An eligible event causes the hook to emit only a routing pointer and exit 2.
  Claude wakes the originating session. Its worker claims and retrieves the body
  through the existing tools; the hook never claims or acknowledges work.
- Presentation shares the lifecycle hooks' deduplication state. Already claimed,
  acknowledged and already presented events do not generate repeated wakeups.
  A later user interaction can recover unfinished work.
- The next normal lifecycle event re-arms the waiter. Pending events on other
  sessions and ordinary unmentioned messages cannot wake this session.

Each waiter runs for at most **24 hours**, with a slightly longer host timeout.
Expiry exits quietly without spending model tokens. The next normal activity
starts another waiter. This is not an unlimited background service: a session
left completely idle past expiry needs interaction to re-arm, or the optional
native channel route. Closing Claude ends delivery.

Status reports `delivery=claude_async_rewake` and `idle_wake=true` only for an
unexpired waiter whose OS lock is still held. A receipt left by a dead process
does not establish readiness. Presence uses the same check. Channel delivery
remains available as an optional alternative and retains Claude's channel
consent requirements.

## Validation

`go test -race ./cmd/tincan` includes tests for successive events, deduplication,
session isolation, claimed/acknowledged work, cancellation, stale receipts and
expiry. Live validation used an isolated Claude Code 2.1.268 CLI with no model
tools or MCP servers, synthetic session-bound inbox snapshots, and the bundled
hook configuration. It responded to events 7 and 8 consecutively from idle,
without user input or channel flags, and re-armed after each response. No messages
were sent to real peers. This verifies the wake mechanism; it does not replace
the existing end-to-end tests of message handling and worker claims.

Upstream contract: [command hook fields](https://code.claude.com/docs/en/hooks#command-hook-fields)
and [background hook behavior](https://code.claude.com/docs/en/hooks#run-hooks-in-the-background).
