import test from "node:test";
import assert from "node:assert/strict";
import { reapplyNode, settleJob } from "./failure-flows.mjs";

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

// A fake backend for reapplyNode: GET /proxy returns proxy, POST /proxy/route
// records its body and starts job "a1", which has ended.
function proxyBackend(proxy) {
  const posted = [];
  const api = async (path, body) => {
    if (path === "/proxy") return proxy;
    if (path === "/proxy/route") { posted.push(body); return { id: "a1" }; }
    if (path === "/status") return { job: { id: "a1", running: false, error: "" } };
    throw new Error(path);
  };
  return { api, posted };
}

test("reapplyNode sends a fixed chain as node IDs", async () => {
  const { api, posted } = proxyBackend({ route: ["relay", "exit"], port: 7890 });
  await reapplyNode(api);
  assert.deepEqual(posted, [{ node_ids: ["relay", "exit"], port: 7890 }]);
});

test("reapplyNode sends a chain with an auto 前置 as hops, not the group's name", async () => {
  const { api, posted } = proxyBackend({
    route: ["CAGE-RELAY-0", "exit"],
    route_hops: [{ relay: { strategy: "url-test", source_id: "air" } }, { node_id: "exit" }],
    port: 7890,
  });
  await reapplyNode(api);
  assert.deepEqual(posted, [{ hops: [{ relay: { strategy: "url-test", source_id: "air" } }, { node_id: "exit" }], port: 7890 }]);
});

test("reapplyNode falls back to the selected node", async () => {
  const { api, posted } = proxyBackend({ selected: "exit", port: 7890 });
  await reapplyNode(api);
  assert.deepEqual(posted, [{ node_ids: ["exit"], port: 7890 }]);
});
