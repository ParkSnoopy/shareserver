// Unit test for encrypted-share metadata clamps.
// Run: node test/crypto.mjs
import { cipherIterations } from "../web/static/js/crypto.js";

let pass = 0, fail = 0;
const ok = (m) => { console.log(`  ok: ${m}`); pass++; };
const err = (m) => { console.log(`  FAIL: ${m}`); fail++; };

function throws(fn) {
	try { fn(); return false; } catch { return true; }
}

if (cipherIterations({ iterations: 600000 }) === 600000) ok("current iteration count accepted");
else err("current iteration count rejected");

if (throws(() => cipherIterations({ iterations: 999999999 }))) ok("huge iteration count rejected");
else err("huge iteration count accepted");

if (throws(() => cipherIterations({ iterations: "bad" }))) ok("non-numeric iteration count rejected");
else err("non-numeric iteration count accepted");

console.log("");
console.log("================================");
console.log(`PASS=${pass}  FAIL=${fail}`);
console.log("================================");
process.exit(fail === 0 ? 0 : 1);
