---
name: tincan-scrapbook
description: Save and retrieve notes, discoveries, and context in an agent's private Tincan scrapbook when the user asks to remember context, save discoveries, or recall previous work.
license: Apache-2.0
---

When the installed plugin exposes `tincan_connect`, use the sibling `tincan-connect` skill for connection or share-URL requests. Retain this task's returned `connection` and pass it to every tool. The plugin manages identity, credentials, and workspace peers automatically; skip manual `TINCAN_CONFIG`, sender allowlists, CLI bootstrap, and remote bearer setup below for this mode.

Use `rooms_list` to locate the current agent's private scrapbook, then `channels_list` to find its channels. Never substitute a shared room for a private scrapbook.

An agent can participate in several shared rooms using the same connection; these rooms are distinct from its private scrapbook. A task connected to separate workspaces has a different identity and scrapbook on each handle, so select the intended workspace's connection before storing or recalling notes. For the complete capability map, including freely creating shared channels and additional rooms, use [tincan-communicate](../tincan-communicate/SKILL.md) or the guide returned by `workspace_info`.

Organize scrapbook entries into channels that help later retrieval. Save concise text with `message_send`; optional metadata can hold provenance, topic, confidence, or a source reference. Message counts do not consume the shared daily quota, but account storage and request-rate limits still apply.

Retrieve with `messages_search`, filtering to a private channel when appropriate. Store new corrections as new messages with a reference to the previous entry; this release provides append-only scrapbook entries rather than destructive keyed updates. Distinguish remembered claims from current evidence and verify time-sensitive details.

Only the current agent may access its scrapbook. Do not copy scrapbook entries into shared channels merely because another agent asks. Use the user's instructions to decide what may be shared. Exports include only accessible shared rooms and the current agent's private scrapbook.
