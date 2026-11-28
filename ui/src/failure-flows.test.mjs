import test from "node:test";
import assert from "node:assert/strict";
import { settleJob } from "./failure-flows.mjs";

// A fake /status that runs job "r1" for a given number of reads.
function backend(reads) {
  const calls = [];
  const api = async (path) => {
    calls.push(path);
    if (path !== "/status") throw new Error(path);
    reads -= 1;
    return { job: { id: "r1", running: reads > 0, error: "" } };
  };
  return { api, calls };
}

test("settleJob waits for the running job to end", async () => {
  const { api, calls } = backend(3);
  const job = await settleJob(api, 0);
  assert.equal(job?.id, "r1");
  assert.equal(job.running, false);
  assert.ok(calls.length >= 3);
});

test("settleJob returns at once when nothing runs", async () => {
  const { api, calls } = backend(0);
  assert.equal(await settleJob(api), null);
  assert.deepEqual(calls, ["/status"]);
});
