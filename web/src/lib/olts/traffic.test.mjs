import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import vm from "node:vm";
import ts from "typescript";

const context = { exports: {} };
vm.runInNewContext(ts.transpileModule(readFileSync(new URL("./traffic.ts", import.meta.url), "utf8"), {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText, context);
const { counterRate, mergeTraffic, formatTraffic } = context.exports;

test("rate needs two valid counters and preserves OLT input/output direction", () => {
  const previous = { t: 10, in_octets: 1000, out_octets: 2000, method: "snmp" };
  const current = { t: 15, in_octets: 2000, out_octets: 4500, method: "snmp" };
  assert.equal(counterRate(null, current), null);
  assert.equal(counterRate(previous, current).in_bps, 1600);
  assert.equal(counterRate(previous, current).out_bps, 4000);
  assert.equal(counterRate(previous, { ...current, in_octets: 0 }), null);
  assert.equal(counterRate(previous, { ...current, method: "cli" }), null);
  assert.equal(counterRate(previous, { ...current, t: 100 }), null);
  assert.equal(counterRate(previous, { ...current, t: 10 }), null);
  assert.equal(counterRate(previous, { ...current, in_octets: Number.MAX_SAFE_INTEGER + 1 }), null);
});

test("history merge is ordered, deduplicated and does not invent missing data", () => {
  const points = mergeTraffic([{ t: 1, in_bps: 3, out_bps: 4 }, { t: 3, in_bps: 1, out_bps: 2 }], [
    { t: 3, in_bps: 5, out_bps: 6 }, { t: 2, in_bps: 0, out_bps: 0 }, { t: 4, in_bps: NaN, out_bps: 0 },
  ], 2);
  assert.equal(points.length, 2);
  assert.equal(points[0].t, 2);
  assert.equal(points[1].in_bps, 5);
  assert.equal(formatTraffic(0), "0 bps");
  assert.equal(formatTraffic(undefined), "-");
});