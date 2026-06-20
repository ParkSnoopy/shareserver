const te = new TextEncoder();
const DEFAULT_ITERATIONS = 600000;
const MIN_ITERATIONS = 100000;
const MAX_ITERATIONS = 1200000;

export function cipherIterations(meta) {
	const n = Number(meta?.iterations ?? DEFAULT_ITERATIONS);
	if (!Number.isInteger(n) || n < MIN_ITERATIONS || n > MAX_ITERATIONS) {
		throw Error("invalid encryption metadata");
	}
	return n;
}

const b64 = (u) => btoa(String.fromCharCode(...u));
const ub64 = (s) => Uint8Array.from(atob(s), (c) => c.charCodeAt(0));
async function key(password, salt, iters) {
	const base = await crypto.subtle.importKey(
		"raw",
		te.encode(password),
		"PBKDF2",
		false,
		["deriveKey"],
	);
	return crypto.subtle.deriveKey(
		{ name: "PBKDF2", hash: "SHA-384", salt, iterations: iters },
		base,
		{ name: "AES-GCM", length: 256 },
		false,
		["encrypt", "decrypt"],
	);
}
export async function encryptBlob(blob, password) {
	const salt = crypto.getRandomValues(new Uint8Array(16)),
		nonce = crypto.getRandomValues(new Uint8Array(12)),
		iterations = DEFAULT_ITERATIONS;
	const k = await key(password, salt, iterations);
	const ct = await crypto.subtle.encrypt(
		{ name: "AES-GCM", iv: nonce },
		k,
		await blob.arrayBuffer(),
	);
	return {
		blob: new Blob([ct], { type: "application/octet-stream" }),
		meta: {
			kdf: "PBKDF2-SHA-384",
			iterations,
			salt: b64(salt),
			cipher: "AES-256-GCM",
			nonce: b64(nonce),
		},
	};
}
export async function decryptBlob(blob, password, meta) {
	const salt = ub64(meta.salt),
		nonce = ub64(meta.nonce);
	try {
		const k = await key(password, salt, cipherIterations(meta));
		const pt = await crypto.subtle.decrypt(
			{ name: "AES-GCM", iv: nonce },
			k,
			await blob.arrayBuffer(),
		);
		return new Blob([pt], { type: "application/zip" });
	} catch {
		throw Error("wrong password");
	}
}
