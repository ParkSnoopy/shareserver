import { describe, expect, test } from "bun:test";
import { cipherIterations } from "../static/js/crypto.js";

describe("cipherIterations", () => {
	test("accepts current iteration count", () => {
		expect(cipherIterations({ iterations: 600000 })).toBe(600000);
	});

	test("rejects huge iteration count", () => {
		expect(() => cipherIterations({ iterations: 999999999 })).toThrow();
	});

	test("rejects non-numeric iteration count", () => {
		expect(() => cipherIterations({ iterations: "bad" })).toThrow();
	});
});
