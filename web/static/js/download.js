const DOWNLOAD_CACHE = "shareserver.downloads.v1";
const DOWNLOAD_PREFIX = "/__download__/";
const WORKER_URL = "/download-sw.js";

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

// canStageDownload verifies browser APIs needed for service-worker download staging.
function canStageDownload() {
	return (
		typeof window !== "undefined" &&
		window.isSecureContext &&
		typeof navigator !== "undefined" &&
		"serviceWorker" in navigator &&
		typeof caches !== "undefined" &&
		typeof Response !== "undefined"
	);
}

let workerReady;
// ensureDownloadWorker registers the single worker once and waits until ready.
async function ensureDownloadWorker() {
	if (!workerReady) {
		workerReady = navigator.serviceWorker
			.register(WORKER_URL, { scope: "/" })
			.then(() => navigator.serviceWorker.ready);
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

// clickDownload triggers a hidden-anchor download using the requested filename.
function clickDownload(href, name) {
	const a = document.createElement("a");
	a.href = href;
	a.download = name;
	a.rel = "noopener";
	a.style.display = "none";
	document.body.append(a);
	a.click();
	setTimeout(() => a.remove(), 1000);
}

// stagedDownloadURL stores one response so Android sees filename headers.
async function stagedDownloadURL(file, name) {
	await ensureDownloadWorker();
	const token = randomToken();
	const url = downloadURLPath(token, name);
	const cache = await caches.open(DOWNLOAD_CACHE);
	await cache.put(
		url,
		new Response(file, {
			headers: {
				"Cache-Control": "no-store",
				"Content-Disposition": contentDispositionFor(name),
				"Content-Type": file.type || "application/octet-stream",
			},
		}),
	);
	return url;
}

// forgetStagedDownload expires unused staged responses after one minute.
function forgetStagedDownload(url) {
	setTimeout(() => {
		caches
			.open(DOWNLOAD_CACHE)
			.then((cache) => cache.delete(url))
			.catch(() => {});
	}, 60000);
}

// saveObjectURL falls back to blob URLs for browsers that honor download names.
function saveObjectURL(file, name) {
	const url = URL.createObjectURL(file);
	clickDownload(url, name);
	setTimeout(() => URL.revokeObjectURL(url), 1000);
}

// saveBlob saves an in-browser entry with its uploaded filename on desktop and Android.
export async function saveBlob(blob, name) {
	const downloadName = safeDownloadName(name);
	const file = namedFile(blob, downloadName);
	if (isAndroid() && canStageDownload()) {
		try {
			const url = await stagedDownloadURL(file, downloadName);
			clickDownload(url, downloadName);
			forgetStagedDownload(url);
			return;
		} catch {}
	}
	saveObjectURL(file, downloadName);
}
