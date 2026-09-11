import { spawn } from 'node:child_process';
import { createInterface } from 'node:readline';
import { EventEmitter } from 'node:events';

export class Sidecar extends EventEmitter {
  constructor(executable, args = []) {
    super();
    this.requests = new Map(); this.sequence = 0; this.closed = false; this.exited = false;
    this.child = spawn(executable, ['sidecar', ...args], { stdio: ['pipe', 'pipe', 'inherit'], windowsHide: true });
    this.lines = createInterface({ input: this.child.stdout });
    this.lines.on('line', line => {
      try {
        const value = JSON.parse(line);
        if (value.event) { this.emit('notice', value); return; }
        const pending = this.requests.get(value.id);
        if (pending) {
          this.requests.delete(value.id); clearTimeout(pending.timer);
          if (value.error) pending.reject(new Error(String(value.error))); else pending.resolve(value.result);
        }
      } catch (error) { this.fail(error); }
    });
    this.child.on('error', error => this.fail(error));
    this.child.on('close', () => { this.exited = true; this.fail(new Error('Tincan sidecar closed')); });
    this.child.stdin.on('error', error => this.fail(error));
  }
  fail(error) {
    if (this.closed) return;
    this.closed = true;
    for (const request of this.requests.values()) { clearTimeout(request.timer); request.reject(error); }
    this.requests.clear(); this.child.stdin.end(); this.emit('closed', error);
  }
  call(method, params = {}) {
    if (this.closed) return Promise.reject(new Error('Tincan sidecar closed'));
    const id = ++this.sequence;
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => { this.requests.delete(id); reject(new Error(`Tincan ${method} timed out; outcome unconfirmed`)); }, 65000);
      this.requests.set(id, { resolve, reject, timer });
      this.child.stdin.write(JSON.stringify({ id, method, params }) + '\n');
    });
  }
  async close() {
    if (this.exited) return;
    const stopped = new Promise(resolve => this.child.once('close', resolve));
    this.child.stdin.end();
    const timer = setTimeout(() => this.child.kill('SIGKILL'), 5000);
    await stopped; clearTimeout(timer);
  }
}
