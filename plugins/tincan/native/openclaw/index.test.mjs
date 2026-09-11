import { test } from 'node:test';
import assert from 'node:assert/strict';
import { EventEmitter } from 'node:events';
import plugin, { OpenClawDelivery } from './index.mjs';

function fixture({ fail = false } = {}) {
  const client = new EventEmitter(); const calls = []; let claimed = false;
  client.call = async (method, params) => {
    calls.push({ method, params });
    if (method === 'claim') {
      if (claimed) return { acquired: false };
      claimed = true;
      return { acquired: true, claim: 'private-claim', event: { payload: { text: 'PEER BODY' } } };
    }
    return {};
  };
  client.close = async () => {};
  const runs = [];
  const runtime = { subagent: {
    run: async params => { runs.push(params); return { runId: 'run', sessionKey: params.sessionKey }; },
    waitForRun: async () => ({ status: fail ? 'error' : 'ok' }),
    getSessionMessages: async () => ({ messages: [{ role: 'assistant', content: [{ type: 'text', text: 'done' }] }] }),
  } };
  const delivery = new OpenClawDelivery(client, runtime, { agentId: 'main', scope: 'Review only' }, { error() {}, warn() {} });
  delivery.connection = 'conn';
  return { client, calls, runs, delivery };
}
test('isolated worker commits only after completion, duplicate notices do not rerun', async () => {
  const { delivery, calls, runs } = fixture();
  await Promise.all([delivery.mention({ connection: 'conn', event_seq: 7 }), delivery.mention({ connection: 'conn', event_seq: 7 })]);
  assert.equal(runs.length, 1);
  assert.match(runs[0].sessionKey, /^agent:main:subagent:tincan-/);
  assert.equal(runs[0].deliver, false);
  assert.ok(!runs[0].message.includes('private-claim'));
  assert.deepEqual(calls.at(-1), { method: 'reply', params: { connection: 'conn', seq: 7, claim: 'private-claim', text: 'done' } });
  await delivery.mention({ connection: 'conn', event_seq: 7 });
  assert.equal(runs.length, 1);
});
test('failures, wrong connections, and nonmentions cannot acknowledge work', async () => {
  const { delivery, calls, client, runs } = fixture({ fail: true });
  await delivery.mention({ connection: 'other', event_seq: 7 });
  client.emit('notice', { event: 'paired', data: { connection: 'conn' } });
  client.emit('notice', { event: 'join_request', data: { connection: 'conn' } });
  assert.equal(runs.length, 0);
  await assert.rejects(delivery.mention({ connection: 'conn', event_seq: 7 }), /failed/);
  assert.equal(calls.filter(c => ['ack', 'reply', 'release'].includes(c.method)).length, 0);
});
test('plugin registration is inert, service starts only after scope configuration', async () => {
  let service;
  plugin.register({ pluginConfig: {}, registerService(value) { service = value; } });
  assert.equal(service.id, 'tincan-listener');
  await service.start({ logger: { info() {} } });
  await service.stop();
});
