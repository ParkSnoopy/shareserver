// Unit test for the Progress reset fix (client-only, not curlable).
// Covers:
//   - failed phase does NOT stick across attempts after reset()
//   - done() after reset renders success (regression: stale "wrong password")
//   - pulse timers cleared on reset (no stale "working" line overwriting done)
//   - done()/fail() cancel pending flush so a fast op isn't overwritten by a
//     stale pulse pending ("working 0%" after "done")
//
// Run: node test/progress.mjs
import { Progress } from "../web/static/js/progress.js";

let pass = 0, fail = 0;
const ok = (m) => { console.log(`  ok: ${m}`); pass++; };
const err = (m) => { console.log(`  FAIL: ${m}`); fail++; };

// DOM shim: capture textContent.
let _text = "";
const el = {
  set textContent(v) { _text = v; },
  get textContent() { return _text; },
};

// performance.now() shim — Progress uses it for speed calc and pulse timing.
globalThis.performance = { now: () => Date.now() };
// setTimeout/setInterval exist in node; clear them on reset is the point.

const p = new Progress(el);

// --- attempt 1: decrypt fails ---
p.pulse("decrypt", 100, "working");
p.fail("decrypt", "wrong password");
if (_text.includes("failed: wrong password")) ok("fail shows 'wrong password'");
else err("fail did not render: " + JSON.stringify(_text));

// --- attempt 2: reset + success ---
p.reset();
if (_text === "") ok("reset clears element");
else err("reset did not clear element: " + JSON.stringify(_text));

p.pulse("decrypt", 100, "working");
p.done("decrypt", 100);
// no pending flush should overwrite done (the race fix)
await new Promise((r) => setTimeout(r, 700)); // past the 500ms flush window
if (/decrypt.*100%.*done/.test(_text)) ok("done shown after retry (no stale failed)");
else err("stale state after retry: " + JSON.stringify(_text));
if (!_text.includes("failed")) ok("no stale 'failed' line after reset");
else err("stale 'failed' line survived reset");

// --- race: pulse then immediate done must not be overwritten by flush ---
p.reset();
p.pulse("unzip", 1000, "working");
p.done("unzip", 1000);
await new Promise((r) => setTimeout(r, 700));
if (/unzip.*100%.*done/.test(_text)) ok("fast op done not overwritten by pulse flush");
else err("pulse flush overwrote done: " + JSON.stringify(_text));
if (!_text.includes("working")) ok("no stale 'working' after done");
else err("stale 'working' survived done");

// --- reset clears active pulse timers (no zombie ticks after reset) ---
p.reset();
p.pulse("download", 500, "working");
const linesBeforeReset = new Map(p.lines);
p.reset();
await new Promise((r) => setTimeout(r, 1200)); // let any zombie tick fire
if (p.lines.size === 0 && _text === "") ok("pulse timers cleared on reset (no zombie ticks)");
else err("zombie pulse tick after reset: lines=" + p.lines.size + " text=" + JSON.stringify(_text));

console.log("");
console.log("================================");
console.log(`PASS=${pass}  FAIL=${fail}`);
console.log("================================");
process.exit(fail === 0 ? 0 : 1);
