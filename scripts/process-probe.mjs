// Test driver only. Exercise the same extensionless Windows .exe resolution used
// by Node/libuv-based MCP clients. This file is never included in the plugin.
import { spawn } from 'node:child_process';
const [command, ...args] = process.argv.slice(2);
const child = spawn(command, args, { stdio: 'inherit', windowsHide: true });
child.on('error', (error) => { console.error(error.message); process.exitCode = 126; });
child.on('exit', (code) => { process.exitCode = code ?? 1; });
