const DOWNLOAD_PREFIX = "/__download__/";
const WORKER_URL = "/download-sw.js";
const DOWNLOAD_MESSAGE_TIMEOUT_MS = 5000;

// replaceControlChars keeps filenames printable without changing normal Unicode.
function replaceControlChars(value) {
	let clean = "";
	for (let i = 0; i < value.length; i++) {
		const code = value.charCodeAt(i);
		clean += code < 32 || code === 127 ? "_" : value[i];
	}
	return clean;
}

// safeDownloadName preserves the uploaded basename while removing path/control risk.
export function safeDownloadName(name) {
	const base =
		String(name || "download")
			.replace(/\\/g, "/")
			.split("/")
			.filter(Boolean)
			.pop() || "download";
	return replaceControlChars(base).trim() || "download";
}

// encodeRFC5987 prepares UTF-8 filenames for Content-Disposition.
function encodeRFC5987(value) {
	return encodeURIComponent(value).replace(
		/[!'()*]/g,
		(ch) => `%${ch.charCodeAt(0).toString(16).toUpperCase()}`,
	);
}

// asciiFilename provides a conservative fallback for older download clients.
function asciiFilename(name) {
	return safeDownloadName(name)
		.replace(/["\\]/g, "_")
		.replace(/[^\x20-\x7E]/g, "_");
}

// contentDispositionFor emits both ASCII and UTF-8 attachment filenames.
export function contentDispositionFor(name) {
	const safe = safeDownloadName(name);
	return `attachment; filename="${asciiFilename(safe)}"; filename*=UTF-8''${encodeRFC5987(safe)}`;
}

// downloadURLPath places the safe filename in the staged service-worker URL.
export function downloadURLPath(token, name) {
	return `${DOWNLOAD_PREFIX}${encodeURIComponent(token)}/${encodeURIComponent(safeDownloadName(name))}`;
}

// isAndroid detects Android clients that need staged downloads for filenames.
function isAndroid() {
	return (
		typeof navigator !== "undefined" &&
		/\bAndroid\b/i.test(navigator.userAgent || "")
	);
}

function canStageDownload() {
	return (
		typeof window !== "undefined" &&
		window.isSecureContext &&
		typeof navigator !== "undefined" &&
		"serviceWorker" in navigator &&
		typeof MessageChannel !== "undefined"
	);
}

// waitForController avoids staging Android downloads before the worker owns page fetches.
function waitForController() {
	if (navigator.serviceWorker.controller) return Promise.resolve();
	return new Promise((resolve, reject) => {
		let timeout = 0;
		const done = () => {
			clearTimeout(timeout);
			resolve();
		};
		timeout = setTimeout(() => {
			navigator.serviceWorker.removeEventListener("controllerchange", done);
			reject(Error("download worker did not control page"));
		}, 5000);
		navigator.serviceWorker.addEventListener("controllerchange", done, {
			once: true,
		});
	});
}

let workerReady;
// ensureDownloadWorker registers the single worker once and waits until ready.
async function ensureDownloadWorker() {
	if (!workerReady) {
		workerReady = navigator.serviceWorker
			.register(WORKER_URL, { scope: "/" })
			.then(async () => {
				const registration = await navigator.serviceWorker.ready;
				await waitForController();
				return registration;
			})
			.catch((err) => {
				workerReady = null;
				throw err;
			});
	}
	return workerReady;
}

// randomToken creates an unguessable cache key for one staged download.
function randomToken() {
	if (globalThis.crypto?.randomUUID) return globalThis.crypto.randomUUID();
	const bytes = new Uint8Array(16);
	if (globalThis.crypto?.getRandomValues) {
		globalThis.crypto.getRandomValues(bytes);
		return [...bytes].map((b) => b.toString(16).padStart(2, "0")).join("");
	}
	return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
}

// namedFile carries the sanitized filename when the browser supports File.
function namedFile(blob, name) {
	const type = blob.type || "application/octet-stream";
	if (typeof File === "function") {
		return new File([blob], name, { type, lastModified: Date.now() });
	}
	return new Blob([blob], { type });
}

// clickDownload triggers a hidden-anchor download. Staged HTTP URLs rely on
// Content-Disposition instead of the download attribute because Android Chrome
// can ignore or cancel synthetic downloads when both are present.
function clickDownload(href, name, useDownloadAttribute = true) {
	const a = document.createElement("a");
	a.href = href;
	if (useDownloadAttribute) a.download = name;
	a.rel = "noopener";
	a.style.display = "none";
	document.body.append(a);
	a.click();
	setTimeout(() => a.remove(), 1000);
}

// postDownloadMessage sends staged file data to the active download worker.
function postDownloadMessage(message) {
	return new Promise((resolve, reject) => {
		const worker = navigator.serviceWorker.controller;
		if (!worker) {
			reject(Error("download worker did not control page"));
			return;
		}
		const channel = new MessageChannel();
		const timeout = setTimeout(() => {
			channel.port1.close();
			reject(Error("download worker did not respond"));
		}, DOWNLOAD_MESSAGE_TIMEOUT_MS);
		channel.port1.onmessage = (event) => {
			clearTimeout(timeout);
			channel.port1.close();
			const data = event.data || {};
			if (data.ok) {
				resolve();
				return;
			}
			reject(Error(data.error || "download worker rejected request"));
		};
		worker.postMessage(message, [channel.port2]);
	});
}

// stagedDownloadURL stores one response so Android sees filename headers.
async function stagedDownloadURL(file, name) {
	await ensureDownloadWorker();
	const url = downloadURLPath(randomToken(), name);
	await postDownloadMessage({
		type: "stage-download",
		url,
		file,
		disposition: contentDispositionFor(name),
		contentType: file.type || "application/octet-stream",
	});
	return url;
}

// forgetStagedDownload expires unused staged responses after one minute.
function forgetStagedDownload(url) {
	setTimeout(() => {
		postDownloadMessage({ type: "forget-download", url }).catch(() => {});
	}, 60000);
}

// objectDownload prepares a blob: URL for clients that honor anchor filenames.
function objectDownload(file, name) {
	const url = URL.createObjectURL(file);
	return {
		href: url,
		downloadName: name,
		cleanup: () => setTimeout(() => URL.revokeObjectURL(url), 1000),
	};
}

// prepareBlobDownload resolves the final href before the user taps the link.
export async function prepareBlobDownload(blob, name) {
	const downloadName = safeDownloadName(name);
	const file = namedFile(blob, downloadName);
	if (isAndroid() && canStageDownload()) {
		try {
			const url = await stagedDownloadURL(file, downloadName);
			return {
				href: url,
				downloadName,
				useDownloadAttribute: false,
				cleanup: () => forgetStagedDownload(url),
			};
		} catch {}
	}
	return objectDownload(file, downloadName);
}

// saveBlob saves an in-browser entry with its uploaded filename.
export async function saveBlob(blob, name) {
	const prepared = await prepareBlobDownload(blob, name);
	clickDownload(
		prepared.href,
		prepared.downloadName,
		prepared.useDownloadAttribute,
	);
	prepared.cleanup();
}
