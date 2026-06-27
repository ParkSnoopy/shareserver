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

// withDevEnv stubs globalThis.document with the dev-mode meta tag so the noble
// fallback is permitted, mirroring the <meta name="shareserver-env" content="dev">
// the server emits in dev mode. Compose with withInsecureContext for the full
// plain-HTTP-LAN-in-dev scenario. Always restores prior document state.
async function withDevEnv(fn) {
	const real = globalThis.document;
	const stub = {
		querySelector: (sel) =>
			sel === 'meta[name="shareserver-env"]'
				? { getAttribute: (attr) => (attr === "content" ? "dev" : null) }
				: null,
	};
	Object.defineProperty(globalThis, "document", {
		value: stub,
		configurable: true,
		writable: true,
	});
	try {
		return await fn();
	} finally {
		if (real === undefined) {
			delete globalThis.document;
		} else {
			Object.defineProperty(globalThis, "document", {
				value: real,
				configurable: true,
				writable: true,
			});
		}
	}
}

describe("pure-JS fallback (insecure context)", () => {
	test("subtle-encrypted blob decrypts via noble fallback", async () => {
		// Encrypt with the native subtle backend (secure context).
		const encrypted = await encryptBlob(new Blob(["zip-bytes"]), "café");

		// Decrypt with subtle disabled -> noble fallback must read the same wire bytes.
		const plain = await withDevEnv(() =>
			withInsecureContext(() => decryptBlob(encrypted.blob, "café", encrypted.meta)),
		);
		expect(await plain.text()).toBe("zip-bytes");
	});

	test("noble-encrypted blob decrypts via subtle backend", async () => {
		// Encrypt with subtle disabled -> noble fallback writes the wire bytes.
		const encrypted = await withDevEnv(() =>
			withInsecureContext(() => encryptBlob(new Blob(["zip-bytes"]), "café")),
		);

		// Decrypt with the native subtle backend (secure context).
		const plain = await decryptBlob(encrypted.blob, "café", encrypted.meta);
		expect(await plain.text()).toBe("zip-bytes");
	});

	test("noble fallback reports backend and rejects wrong password", async () => {
		const encrypted = await encryptBlob(new Blob(["zip-bytes"]), "café");
		const events = [];
		await expect(
		withDevEnv(() =>
			withInsecureContext(() =>
				decryptBlob(encrypted.blob, "wrong", encrypted.meta, {
					onDebug: (event, data) => events.push({ event, data }),
				}),
			),
		),
		).rejects.toThrow("wrong password");
		expect(events.map((e) => e.event)).toContain("crypto-fallback");
		expect(events.find((e) => e.event === "crypto-fallback").data).toEqual({
			backend: "noble",
		});
		// debug payload must not leak the password
		expect(JSON.stringify(events)).not.toContain("café");
	});

	test("fallback-load event precedes crypto-fallback on first noble fetch", async () => {
		// Fresh module import so nobleBackendPromise starts null and the load
		// event fires; a cached nobleBackendPromise skips the fetch (and event).
		const fresh = await import(`../static/js/crypto.js?fresh=${Date.now()}`);
		const encrypted = await fresh.encryptBlob(new Blob(["zip-bytes"]), "café");
		const events = [];
		await expect(
		withDevEnv(() =>
			withInsecureContext(() =>
				fresh.decryptBlob(encrypted.blob, "wrong", encrypted.meta, {
					onDebug: (event, data) => events.push({ event, data }),
				}),
			),
		),
		).rejects.toThrow("wrong password");
		const names = events.map((e) => e.event);
		expect(names).toContain("crypto-fallback-load");
		expect(names.indexOf("crypto-fallback")).toBeGreaterThan(
			names.indexOf("crypto-fallback-load"),
		);
		expect(
			events.find((e) => e.event === "crypto-fallback-load").data,
		).toEqual({ backend: "noble" });
		expect(JSON.stringify(events)).not.toContain("café");
	});
	test("prod blocks noble fallback on insecure context", async () => {
		// Fresh module import so nobleBackendPromise starts null. Without the
		// dev meta tag, selectBackend must fail closed instead of attempting the
		// noble fallback, even though crypto.subtle is absent.
		const fresh = await import(`../static/js/crypto.js?fresh=${Date.now()}`);
		const encrypted = await fresh.encryptBlob(new Blob(["zip-bytes"]), "café");
		await expect(
			withInsecureContext(() =>
				fresh.decryptBlob(encrypted.blob, "café", encrypted.meta),
			),
		).rejects.toThrow(/HTTPS or localhost/);
	});
});
