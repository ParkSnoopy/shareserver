import { describe, expect, test } from "bun:test";
import {
	cipherIterations,
	decryptBlob,
	downloadPasswordHash,
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
	test("authorization hash is domain-separated and Unicode-normalized", async () => {
		const composed = await downloadPasswordHash("café");
		const decomposed = await downloadPasswordHash("cafe\u0301");
		expect(composed).toBe(decomposed);
		expect(composed).toBe("4CiVpafBV8PnCuswV0FLZKV/wE/aRw4ygv530eT3+aw=");
		expect(composed).not.toContain("café");
	});

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

async function serverCipherFixture(plaintext, password) {
	const encoder = new TextEncoder();
	const salt = new Uint8Array(16).fill(7);
	const prefix = new Uint8Array(8).fill(9);
	const base = await crypto.subtle.importKey(
		"raw",
		encoder.encode(password.normalize("NFC")),
		"PBKDF2",
		false,
		["deriveKey"],
	);
	const key = await crypto.subtle.deriveKey(
		{ name: "PBKDF2", hash: "SHA-384", salt, iterations: 600000 },
		base,
		{ name: "AES-GCM", length: 256 },
		false,
		["encrypt", "decrypt"],
	);
	const bytes =
		plaintext instanceof Uint8Array ? plaintext : encoder.encode(plaintext);
	const pieces = [];
	let bodySize = 0;
	for (let offset = 0, index = 0; offset < bytes.byteLength; index++) {
		const size = Math.min(1 << 20, bytes.byteLength - offset);
		const final = offset + size === bytes.byteLength;
		const header = new Uint8Array(5);
		header[0] = final ? 1 : 0;
		new DataView(header.buffer).setUint32(1, size);
		const nonce = new Uint8Array(12);
		nonce.set(prefix);
		new DataView(nonce.buffer).setUint32(8, index);
		const encrypted = new Uint8Array(
			await crypto.subtle.encrypt(
				{ name: "AES-GCM", iv: nonce, additionalData: header },
				key,
				bytes.subarray(offset, offset + size),
			),
		);
		pieces.push(header, encrypted);
		bodySize += header.byteLength + encrypted.byteLength;
		offset += size;
	}
	const body = new Uint8Array(bodySize);
	let bodyOffset = 0;
	for (const piece of pieces) {
		body.set(piece, bodyOffset);
		bodyOffset += piece.byteLength;
	}
	const b64 = (value) => btoa(String.fromCharCode(...value));
	return {
		body,
		meta: {
			kdf: "PBKDF2-SHA-384",
			iterations: 600000,
			salt: b64(salt),
			cipher: "AES-256-GCM-CHUNKED",
			nonce: b64(prefix),
			chunk_size: 1 << 20,
		},
	};
}

describe("server-encrypted payload", () => {
	test("decrypts authenticated framed ciphertext", async () => {
		const encrypted = await serverCipherFixture(
			"server zip bytes",
			"cafe\u0301",
		);
		const plain = await decryptBlob(encrypted.body, "café", encrypted.meta, {
			returnBytes: true,
		});
		expect(new TextDecoder().decode(plain)).toBe("server zip bytes");
	});

	test("rejects truncated framed ciphertext", async () => {
		const encrypted = await serverCipherFixture("server zip bytes", "password");
		await expect(
			decryptBlob(
				encrypted.body.subarray(0, encrypted.body.length - 1),
				"password",
				encrypted.meta,
			),
		).rejects.toThrow("wrong password");
	});

	test("decrypts multiple authenticated frames", async () => {
		const source = new Uint8Array((1 << 20) + 17);
		for (let i = 0; i < source.length; i++) source[i] = i % 251;
		const encrypted = await serverCipherFixture(source, "password");
		const plain = await decryptBlob(
			encrypted.body,
			"password",
			encrypted.meta,
			{
				returnBytes: true,
			},
		);
		expect(plain).toEqual(source);
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
