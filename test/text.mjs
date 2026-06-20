// Unit test for text preview normalization.
// Canonical Hangul Jamo should display composed; compatibility Jamo must not be
// guessed/re-written into syllables.
// Run: node test/text.mjs
import { normalizeText } from "../web/static/js/text.js";

let pass = 0, fail = 0;
const ok = (m) => { console.log(`  ok: ${m}`); pass++; };
const err = (m) => { console.log(`  FAIL: ${m}`); fail++; };

const canonical = "\u1100\u1161\u1102\u1161\u1103\u1161\u1105\u1161";
const compatibility = "ㄱㅏㄴㅏㄷㅏㄹㅏ";

if (normalizeText(canonical) === "가나다라") ok("canonical Jamo compose through NFC");
else err("canonical Jamo did not compose: " + JSON.stringify(normalizeText(canonical)));

if (normalizeText(compatibility) === compatibility) ok("compatibility Jamo preserved, no custom rewrite");
else err("compatibility Jamo was rewritten: " + JSON.stringify(normalizeText(compatibility)));

console.log("");
console.log("================================");
console.log(`PASS=${pass}  FAIL=${fail}`);
console.log("================================");
process.exit(fail === 0 ? 0 : 1);
