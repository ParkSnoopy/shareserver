const te = new TextEncoder();
const DEFAULT_ITERATIONS = 600000;
const MIN_ITERATIONS = 100000;
const MAX_ITERATIONS = 1200000;

// cipherIterations validates PBKDF2 cost metadata before decrypting.
export function cipherIterations(meta) {
	const n = Number(meta?.iterations ?? DEFAULT_ITERATIONS);
	if (!Number.isInteger(n) || n < MIN_ITERATIONS || n > MAX_ITERATIONS) {
		throw Error("invalid encryption metadata");
	}
	return n;
}

const b64 = (u) => btoa(String.fromCharCode(...u));
const ub64 = (s) => Uint8Array.from(atob(s), (c) => c.charCodeAt(0));

function unsupportedCryptoError(cause) {
	const err = Error("browser crypto requires HTTPS or localhost");
	err.name = "UnsupportedCryptoError";
	err.code = "unsupported_crypto";
	if (cause) err.cause = cause;
	return err;
}

// hasSubtle is true only on secure contexts (HTTPS or localhost). Chrome
// disables crypto.subtle on insecure origins, so encrypted shares opened over
// plain-HTTP LAN IPs cannot be decrypted — the page shows a clear error
// instead of attempting a pure-JS fallback that would crash memory-constrained
// mobile browsers on large archives.
function hasSubtle() {
	return Boolean(globalThis.crypto?.subtle);
}

// passwordForms tries canonically equivalent strings so mobile keyboards and
// desktop keyboards derive the same key for the same visible password. New
// encryption uses NFC; decrypt keeps raw/NFD fallbacks for older shares.
function passwordForms(password) {
	const raw = String(password);
	const forms = [{ label: "nfc", value: raw.normalize("NFC") }];
	if (!forms.some((form) => form.value === raw)) {
		forms.push({ label: "raw", value: raw });
	}
	const nfd = raw.normalize("NFD");
	if (!forms.some((form) => form.value === nfd)) {
		forms.push({ label: "nfd", value: nfd });
	}
	return forms;
}
function passwordShape(value, raw) {
	return {
		utf16Length: value.length,
		codePointLength: [...value].length,
		utf8Bytes: te.encode(value).length,
		sameAsRaw: value === raw,
		nfcChanged: value.normalize("NFC") !== value,
		nfdChanged: value.normalize("NFD") !== value,
	};
}

// The crypto backend derives an AES-256 key from a password and runs AES-GCM.
// Wire bytes: PBKDF2-HMAC-SHA-384 -> 32-byte key, AES-256-GCM, 12-byte nonce,
// no AAD, 16-byte tag appended. Only crypto.subtle is used — it is native,
// fast, and available on all secure contexts.

// subtleBackend wraps window.crypto.subtle (fast, native). Key handle: { subtle }.
const subtleBackend = {
	async deriveKey(passwordBytes, salt, iters) {
		const subtle = globalThis.crypto.subtle;
		const base = await subtle.importKey("raw", passwordBytes, "PBKDF2", false, [
			"deriveKey",
		]);
		return {
			subtle: await subtle.deriveKey(
				{ name: "PBKDF2", hash: "SHA-384", salt, iterations: iters },
				base,
				{ name: "AES-GCM", length: 256 },
				false,
				["encrypt", "decrypt"],
			),
		};
	},
	encrypt(kh, nonce, plaintext) {
		return globalThis.crypto.subtle
			.encrypt({ name: "AES-GCM", iv: nonce }, kh.subtle, plaintext)
			.then((buf) => new Uint8Array(buf));
	},
	decrypt(kh, nonce, ciphertext) {
		return globalThis.crypto.subtle
			.decrypt({ name: "AES-GCM", iv: nonce }, kh.subtle, ciphertext)
			.then((buf) => new Uint8Array(buf));
	},
};

// selectBackend returns the native subtle backend on secure contexts. On
// insecure contexts (plain-HTTP LAN, where Chrome disables crypto.subtle) it
// fails closed with UnsupportedCryptoError so operators must serve HTTPS.
async function selectBackend() {
	if (hasSubtle()) return subtleBackend;
	throw unsupportedCryptoError();
}

// randomBytes generates salt/nonce material. crypto.getRandomValues is
// available on insecure contexts too (only crypto.subtle is gated).
function randomBytes(n) {
	const api = globalThis.crypto;
	if (!api || typeof api.getRandomValues !== "function") {
		throw unsupportedCryptoError();
	}
	return api.getRandomValues(new Uint8Array(n));
}

// encryptBlob wraps a zip blob with AES-GCM and returns safe metadata.
export async function encryptBlob(blob, password) {
	const backend = await selectBackend();
	const salt = randomBytes(16),
		nonce = randomBytes(12),
		iterations = DEFAULT_ITERATIONS;
	const k = await backend.deriveKey(
		te.encode(passwordForms(password)[0].value),
		salt,
		iterations,
	);
	const ct = await backend.encrypt(
		k,
		nonce,
		new Uint8Array(await blob.arrayBuffer()),
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

// decryptBlob opens an encrypted share zip or reports a generic wrong-password error.
// Accepts a Blob or a pre-read Uint8Array to avoid an ArrayBuffer roundtrip when
// the caller already has the bytes in memory.
export async function decryptBlob(source, password, meta, options = {}) {
	const salt = ub64(meta.salt),
		nonce = ub64(meta.nonce),
		iterations = cipherIterations(meta),
		rawPassword = String(password);
	let body = source instanceof Uint8Array ? source : new Uint8Array(await source.arrayBuffer());
	const subtleCrypto = hasSubtle();
	options.onDebug?.("crypto-input", {
		cipher: meta?.cipher || "",
		kdf: meta?.kdf || "",
		iterations,
		saltBytes: salt.byteLength,
		nonceBytes: nonce.byteLength,
		blobBytes: body.byteLength,
		subtleCrypto,
	});
	const backend = await selectBackend(options.onDebug);
	let lastErr = null;
	for (const passwordForm of passwordForms(rawPassword)) {
		options.onDebug?.("crypto-attempt", {
			form: passwordForm.label,
			password: passwordShape(passwordForm.value, rawPassword),
		});
		try {
			const k = await backend.deriveKey(
				te.encode(passwordForm.value),
				salt,
				iterations,
			);
			const pt = await backend.decrypt(k, nonce, body);
			body = null;
			options.onDebug?.("crypto-attempt-result", {
				form: passwordForm.label,
				ok: true,
			});
			if (options.returnBytes) return pt;
			return new Blob([pt], { type: "application/zip" });
		} catch (err) {
			lastErr = err;
			options.onDebug?.("crypto-attempt-result", {
				form: passwordForm.label,
				ok: false,
				errorName: err?.name || "",
				errorMessage: err?.message || String(err),
			});
		}
	}
	if (
		lastErr?.name === "UnsupportedCryptoError" ||
		lastErr?.name === "NotSupportedError"
	) {
		throw lastErr;
	}
	throw Error("wrong password");
}
