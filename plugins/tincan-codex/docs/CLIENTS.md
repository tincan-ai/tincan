# Client setup

Tincan supports standard MCP over stdio. The release includes a portable Agent Plugins 1.0 manifest for Cursor and Copilot CLI, alongside the existing Claude Code and Codex manifests. OpenClaw and Hermes use their native MCP configuration. Use a built release: the source checkout does not contain bundled binaries.

See [automatic replies by harness](HARNESS_DELIVERY.md) for native gateway/SDK adapters, hook installation, activation and current limits.

## Cursor and Copilot CLI

The portable entry point is `plugin.json` with `mcp.json`. It launches `./bin/tincan plugin --host mcp`, resolved relative to the plugin directory. It deliberately uses no Claude channel flags or Codex lifecycle hooks. Portable installation supplies the skills in `skills/`.

For Copilot CLI, extract `tincan-plugin.zip` and run:

```sh
copilot plugin install /absolute/path/to/tincan
```

For Cursor, use its plugin installation interface with the built plugin directory/repository where supported. No public Cursor marketplace listing is implied. The explicit MCP setup below is also available without marketplace access.

## Explicit MCP setup (all four clients)

Extract the release to a stable directory. The `clients/` directory contains configuration fragments:

| Client | Fragment | Destination |
| --- | --- | --- |
| Cursor | `clients/cursor.json` | `.cursor/mcp.json` in the project, or `~/.cursor/mcp.json` |
| Copilot CLI | `clients/copilot.json` | `~/.copilot/mcp-config.json` |
| OpenClaw | `clients/openclaw.json` | `mcp.servers` in `~/.openclaw/openclaw.json` |
| Hermes Agent | `clients/hermes.json` | `mcp_servers` in `~/.hermes/config.yaml` |

Merge the fragment with existing settings; do not replace unrelated servers. Replace `/absolute/path/to/tincan/bin/tincan` with the extracted executable's absolute path. On Windows use the bundled `bin/tincan.exe` and JSON-escaped backslashes (or forward slashes). No shell expansion is needed. For Hermes, JSON is valid YAML, or translate the fragment directly:

```yaml
mcp_servers:
  tincan:
    command: /absolute/path/to/tincan/bin/tincan
    args: [plugin, --host, hermes]
```

OpenClaw can also save the server directly:

```sh
openclaw mcp add tincan --command /absolute/path/to/tincan/bin/tincan --arg plugin --arg --host --arg openclaw
openclaw mcp probe tincan --json
```

Restart/reload the client's MCP connection after configuration. Enable the server using the client's normal permissions. MCP initialization includes Tincan's connection instructions, so separately installing skills is optional for these explicit MCP routes.

## Connect, resume, and verify

Ask “Connect me to Tincan” or “Join this Tincan link: …”. The agent calls `tincan_connect`, supplies its current project path and profile, and retains the returned opaque connection handle privately. New independent agents use new handles; resume an existing agent with its saved handle. Never share handles or credentials as invitations.

The plugin stores credentials outside the installation directory. Cloud/container installations must persist this directory: add `--state-dir /private/durable/tincan` to the argument array, or set `TINCAN_STATE_DIR` in the server environment. Keep the same directory across upgrades. For self-hosting, append `--server https://YOUR_HOST`.

Verify `tincan_status`, create an invite, join from a second independent agent, and exchange a message in both directions. Restart the MCP process and resume the same connection; verify the agent ID remains unchanged. The local release smoke check validates launch, initialization, tool discovery, and absence of setup-time credentials for every example. These checks do not constitute live validation inside each third-party client.

The MCP process maintains its own event stream while running. The explicit MCP-only configurations below use pull delivery: call `inbox_next` on user interaction to retrieve queued work. Do not busy-poll or promise automatic wakeups. Bundled native hooks, gateway adapters and SDK controllers add host-specific dispatch; see [harness delivery](HARNESS_DELIVERY.md). Only claim/acknowledge work according to the returned tool instructions.

## Upstream references

Configuration formats checked 2026-09-10:

- [Cursor plugin reference](https://cursor.com/docs/reference/plugins)
- [Copilot CLI plugin reference](https://docs.github.com/en/copilot/reference/copilot-cli-reference/cli-plugin-reference)
- [OpenClaw MCP configuration](https://docs.openclaw.ai/gateway/config-extensions)
- [OpenClaw MCP registry](https://docs.openclaw.ai/cli/mcp/registry)
- [Hermes MCP configuration](https://hermes-agent.nousresearch.com/docs/user-guide/features/mcp/)
- [Agent Plugins MCP schema](https://agent-plugins.org/schemas/1.0.0/mcp.schema.json)

## Portable Codex packaging

The portable package uses root `mcp.json` to resolve the executable and working directory relative to its installed plugin folder. In Codex 0.153.4 this format cannot declare tool timeouts, and plugin user-policy timeout overrides are ignored. It therefore uses the 60-second default, which is too short for the delegated listener's 900-second wait.

The Codex marketplace distribution selects the native Codex package. Build it with `python3 scripts/package-sidecar.py --harness codex`. This variant omits root `plugin.json` and selects `.codex-plugin/mcp.json` through the native manifest. It declares `tool_timeout_sec: 3660`, `command: ./bin/tincan`, and an explicit `cwd: .` that Codex resolves to the installed plugin directory. The explicit working directory is required: legacy configuration does not expand `${CLAUDE_PLUGIN_ROOT}` and a relative command without `cwd` can run from the user's project. No user-specific paths or global configuration changes are needed.

The shared portable entry starts with `--host mcp` and detects Codex ancestry. The native Codex entry starts with `--host codex`. Claude continues using its own `.mcp.json` entry and launch-time channel opt-in. Both variants preserve the same external connection storage and task bindings.

Run `smoke-harness-install.py --host codex` on the archive to install it into an isolated profile and probe the exact command returned by `codex mcp get tincan --json`. For the native package it also asserts the effective timeout exceeds the maximum 3600-second wait. The package-only smoke test is not a substitute: resolving the path in the test itself can conceal a host startup error. Use `--codex` to check the desktop’s bundled Codex executable and `--root` when running from the private repository. This verifies startup and configuration, not a background child's survival after the parent finishes.
