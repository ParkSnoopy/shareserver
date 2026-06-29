import { describe, expect, test } from "bun:test";
import {
	cipherIterations,
	decryptBlob,
	encryptBlob,
} from "../static/js/crypto.js";

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

describe("password canonicalization", () => {
	test("decrypt accepts mobile and desktop Unicode forms", async () => {
		const composed = "café";
		const decomposed = "cafe\u0301";
		const plain = new Blob(["zip-bytes"]);

		const desktopEncrypted = await encryptBlob(plain, composed);
		expect(
			await (
				await decryptBlob(
					desktopEncrypted.blob,
					decomposed,
					desktopEncrypted.meta,
				)
			).text(),
		).toBe("zip-bytes");

		const mobileEncrypted = await encryptBlob(plain, decomposed);
		expect(
			await (
				await decryptBlob(mobileEncrypted.blob, composed, mobileEncrypted.meta)
			).text(),
		).toBe("zip-bytes");
	});

	test("decrypt debug reports safe attempt metadata", async () => {
		const encrypted = await encryptBlob(new Blob(["zip-bytes"]), "café");
		const events = [];

		await decryptBlob(encrypted.blob, "cafe\u0301", encrypted.meta, {
			onDebug: (event, data) => events.push({ event, data }),
		});

		expect(events.map((entry) => entry.event)).toContain("crypto-input");
		expect(events.map((entry) => entry.event)).toContain("crypto-attempt");
		expect(JSON.stringify(events)).not.toContain("café");
		expect(JSON.stringify(events)).not.toContain("cafe\u0301");
	});

	test("returnBytes returns Uint8Array instead of Blob", async () => {
		const encrypted = await encryptBlob(new Blob(["zip-bytes"]), "pw");
		const result = await decryptBlob(encrypted.blob, "pw", encrypted.meta, {
			returnBytes: true,
		});
		expect(result).toBeInstanceOf(Uint8Array);
		expect(new TextDecoder().decode(result)).toBe("zip-bytes");
	});

	test("accepts pre-read Uint8Array source without Blob roundtrip", async () => {
		const encrypted = await encryptBlob(new Blob(["zip-bytes"]), "pw");
		const source = new Uint8Array(await encrypted.blob.arrayBuffer());
		const result = await decryptBlob(source, "pw", encrypted.meta, {
			returnBytes: true,
		});
		expect(result).toBeInstanceOf(Uint8Array);
		expect(new TextDecoder().decode(result)).toBe("zip-bytes");
	});

	test("decrypt rejects wrong password", async () => {
		const encrypted = await encryptBlob(new Blob(["zip-bytes"]), "correct");
		await expect(
			decryptBlob(encrypted.blob, "wrong", encrypted.meta),
		).rejects.toThrow("wrong password");
	});
});

// withInsecureContext runs `fn` with globalThis.crypto replaced by a stub that
// has getRandomValues but no crypto.subtle, simulating a plain-HTTP LAN phone
// where Chrome disables WebCrypto. Always restores the real crypto.
async function withInsecureContext(fn) {
	const real = globalThis.crypto;
	const stub = { getRandomValues: real.getRandomValues.bind(real) };
	Object.defineProperty(globalThis, "crypto", {
		value: stub,
		configurable: true,
		writable: true,
	});
	try {
		return await fn();
	} finally {
		Object.defineProperty(globalThis, "crypto", {
			value: real,
			configurable: true,
			writable: true,
		});
	}
}

describe("insecure context", () => {
	test("encrypt fails closed without crypto.subtle", async () => {
		await expect(
			withInsecureContext(() => encryptBlob(new Blob(["zip-bytes"]), "pw")),
		).rejects.toThrow(/HTTPS or localhost/);
	});

	test("decrypt fails closed without crypto.subtle", async () => {
		const encrypted = await encryptBlob(new Blob(["zip-bytes"]), "pw");
		await expect(
			withInsecureContext(() =>
				decryptBlob(encrypted.blob, "pw", encrypted.meta),
			),
		).rejects.toThrow(/HTTPS or localhost/);
	});
});
