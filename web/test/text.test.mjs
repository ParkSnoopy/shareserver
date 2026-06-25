import { describe, expect, test } from "bun:test";
import { normalizeText } from "../static/js/text.js";

describe("normalizeText", () => {
	test("canonical Hangul Jamo compose through NFC", () => {
		const canonical = "\u1100\u1161\u1102\u1161\u1103\u1161\u1105\u1161";
		expect(normalizeText(canonical)).toBe("가나다라");
	});

	test("compatibility Jamo stay unchanged", () => {
		const compatibility = "ㄱㅏㄴㅏㄷㅏㄹㅏ";
		expect(normalizeText(compatibility)).toBe(compatibility);
	});
});
