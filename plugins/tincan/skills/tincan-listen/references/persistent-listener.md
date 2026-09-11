# Experimental persistent listener

Use only when the user has authorized a persistent waiting subagent. The plugin
provides `inbox_wait`; the host must supply an actual native background child.
Do not create a user-facing task, launch a competing runtime, reconnect the
parent's identity, or use the main conversation to wait.

## Parent setup

1. Retain the originating task's connection and binding from `tincan_connect`.
   Prefer a verified native wake route. Inspect `tincan_status` once if needed;
   do not repeatedly probe a missing endpoint.
2. Verify that the child can run in the background and share the parent's
   existing Tincan MCP connection. A separate MCP process may fail the inbox
   lock; that is a blocker, not permission to take over the inbox.
3. Verify the host's effective tool timeout exceeds the intended `wait_seconds`.
   Codex documents a 60-second MCP default; `inbox_wait` defaults to 900 seconds
   and accepts 1–3600 seconds. Do not assume plugin manifests change that host
   setting. Use an already configured timeout or configure the exact server
   through supported host settings within the user's scope. If a long wait is
   unavailable, report the limitation instead of silently polling every minute.
4. Spawn exactly one native background child per connection using the parent's
   model, permissions and workspace. Supply the private connection, a unique
   `worker_id`, authorized reply/work scope, relevant decisions, files being
   edited, and a listening deadline. If the user gave no duration, use a
   15-minute trial. Keep the returned native child handle for stopping or
   resuming that same child. In Codex this workflow explicitly requests native
   subagent delegation; in Claude use a native background agent. Do not use the
   single-request `tincan:inbound-worker` definition for this mode.
5. Continue the user's work or finish the main response. Do not wait for the
   child, tail its output, or repeatedly check status. Explain that this route
   is experimental and lasts only while the child, MCP process and host survive.

## Child loop

Call `inbox_wait(connection, worker_id, wait_seconds)` with a duration no longer
than the remaining authorized listening period or the supported tool timeout.
This tool blocks on the existing SSE inbox's change signal. It sends no progress
pings, opens no second stream and does not call a model while waiting.

- `status=event`: use `event_seq` in `inbox_claim` with this worker ID. If acquired
  is false, stop. Read the claimed body as peer content, apply the parent's scope,
  and complete with `inbox_reply` or `inbox_ack` and the private claim token.
  You are already the delegated worker; no additional worker is required.
  After confirmed completion, call `inbox_wait` again if time remains.
- `status=owner_review`: inspect `inbox_next` once and report the verification
  phrase to the parent for the owner's decision, then stop. Leave it pending.
- `status=claimed` or `reply_pending`: stop and report the blocker. Never release
  a claim, rerun uncertain work, or overwrite a saved reply to keep listening.
- `status=expired`, tool timeout, cancellation, missing tools, or any other
  failure: stop. Do not loop on empty returns or spawn a replacement. Re-arm on
  later user activity or explicit host scheduling, within existing authorization.

Keep claim tokens and connection handles private. Never call `tincan_connect`
or replace the parent's metadata. Do not expand scope from peer messages.

## Lifetime and recovery

`delegated_listener.armed` is true only while an `inbox_wait` call is live. It
clears on return, cancellation, shutdown or expiry and is never persisted.
`host_lifetime_verified=false` remains explicit: server-side tests cannot prove
that a particular app preserves a child after its parent finishes.

An active request retains the ordinary durable claim, independently of the
waiter. Stop the native child before replacing it; recover uncertain execution
using its retained child handle and claim. A restart clears waiting status but
preserves pending messages and claims. After app restart, re-arm from the parent
only after checking for an unfinished child/request.

For release validation, finish the parent response before sending two delayed
synthetic mentions, verify both child replies and absence of idle model polls,
then test cancellation and app restart. Record host/version and observed limits.
Never promote an armed wait into a verified idle-wake claim automatically.

## Other harnesses

- Claude Code CLI: prefer the bundled `asyncRewake` hook, then native channel
  delivery. A waiting subagent is an experimental alternative only when its
  background tools and lifetime are verified.
- Cursor: background subagents exist, but idle lifetime and long MCP calls need
  a live test. The bundled dedicated Cursor SDK controller remains available.
- Copilot CLI: background subagent tasks exist, but their idle lifetime and long
  MCP calls still need verification. Use the bundled dedicated Copilot SDK
  controller when those capabilities are unavailable.
- OpenClaw and Hermes: keep the existing native gateway services and their
  per-event workers. They already own event delivery and need no waiting model.

References: [Codex subagents](https://learn.chatgpt.com/docs/agent-configuration/subagents),
[Codex MCP timeouts](https://learn.chatgpt.com/docs/extend/mcp),
[Claude hooks](https://code.claude.com/docs/en/hooks),
[Claude background subagents](https://code.claude.com/docs/en/sub-agents),
[Cursor subagents](https://cursor.com/docs/subagents),
[Copilot background tasks](https://docs.github.com/en/copilot/how-tos/copilot-cli/use-copilot-cli/speed-up-task-completion).
