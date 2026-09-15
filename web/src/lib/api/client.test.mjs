import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import vm from "node:vm";
import ts from "typescript";

const source = ts.transpileModule(readFileSync(new URL("./client.ts", import.meta.url), "utf8"), {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText;

function loadClient(fetch, overrides = {}) {
  const context = {
    exports: {},
    require: () => ({ APIError: Error }),
    document: { cookie: "" },
    Headers, AbortController, Error, TypeError, setTimeout, clearTimeout,
    fetch,
    ...overrides,
  };
  vm.runInNewContext(source, context);
  return context.exports.clientAPI;
}

test("caller cancellation reaches fetch and is not retried", async () => {
  let attempts = 0;
  let signal;
  const controller = new AbortController();
  const client = loadClient((_path, init) => {
    attempts += 1;
    signal = init.signal;
    return new Promise((_resolve, reject) => {
      signal.addEventListener("abort", () => reject(Object.assign(new Error("aborted"), { name: "AbortError" })), { once: true });
    });
  });
  const request = client("/api/v1/olts/test/chassis", { signal: controller.signal });
  assert.equal(signal.aborted, false);
  controller.abort();
  assert.equal(signal.aborted, true);
  await assert.rejects(request, /timeout/i);
  assert.equal(attempts, 1);
});

test("explicit OLT deadline is not replaced by the shorter helper timeout", async () => {
  const controller = new AbortController();
  let deadline = 0;
  const client = loadClient(async (_path, init) => {
    assert.equal(init.signal.aborted, false);
    return { ok: true, status: 200, json: async () => ({ sample: {} }) };
  }, { setTimeout: (_callback, milliseconds) => { deadline = milliseconds; } });
  await client("/api/v1/olts/test/onu-detail-cli?force_cli=1", { signal: controller.signal, timeoutMs: 60000 });
  assert.equal(deadline, 60000);
});

test("ordinary requests retain a bounded internal signal", async () => {
  let bounded = false;
  const client = loadClient(async (_path, init) => {
    bounded = init.signal instanceof AbortSignal;
    return { ok: true, status: 200, json: async () => ({ items: [] }) };
  });
  await client("/api/v1/olts");
  assert.equal(bounded, true);
});