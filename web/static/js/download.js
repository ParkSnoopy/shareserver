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

// downloadURLPath keeps staged client-side files under the visible share URL.
export function downloadURLPath(shareID, name) {
	return `/s/${encodeURIComponent(shareID)}/f/${encodeURIComponent(safeDownloadName(name))}`;
}

export function canStageDownload() {
	return (
		typeof window !== "undefined" &&
		window.isSecureContext &&
		typeof navigator !== "undefined" &&
		!/\bAndroid\b/i.test(navigator.userAgent || "") &&
		"serviceWorker" in navigator &&
		typeof MessageChannel !== "undefined"
	);
}

// waitForController avoids staging downloads before the worker owns page fetches.
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

// namedFile carries the sanitized filename when the browser supports File.
function namedFile(blob, name) {
	const type = blob.type || "application/octet-stream";
	if (typeof File === "function") {
		return new File([blob], name, { type, lastModified: Date.now() });
	}
	return new Blob([blob], { type });
}

// clickDownload triggers a hidden-anchor download. Staged HTTP URLs rely on
// Content-Disposition so the URL stays under /s/{shareID}/f/{filename}.
export function clickDownload(href, name, useDownloadAttribute = true) {
	const a = document.createElement("a");
	a.href = href;
	if (useDownloadAttribute) a.download = name;
	a.rel = "noopener";
	a.style.display = "none";
	document.body.append(a);
	a.click();
	setTimeout(() => a.remove(), 1000);
}

// clickPreparedDownload follows the prepared transfer URL without replacing the
// visible link href. In insecure LAN browsers the visible href can stay under
// /s/{shareID}/f/{filename} while the hidden click uses a blob: URL fallback.
// Staged service-worker URLs (no download attribute) are keyed by pathname in
// the worker, so a per-click cache-buster makes every tap a distinct
// navigation; without it browsers dedupe a same-URL reload and the second
// download never starts.
export function clickPreparedDownload(prepared) {
	let href = prepared.clickHref || prepared.href;
	if (prepared.useDownloadAttribute === false && href) {
		href = `${href}${href.includes("?") ? "&" : "?"}d=${Date.now()}`;
	}
	clickDownload(href, prepared.downloadName, prepared.useDownloadAttribute);
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

// stagedDownloadURL stores a response at /s/{shareID}/f/{filename}.
async function stagedDownloadURL(file, shareID, name, options = {}) {
	const debug = typeof options.onDebug === "function" ? options.onDebug : () => {};
	await ensureDownloadWorker();
	const url = downloadURLPath(shareID, name);
	debug("download-stage-start", {
		url,
		name,
		bytes: file.size || 0,
		contentType: file.type || "application/octet-stream",
		serviceWorkerController: Boolean(navigator.serviceWorker?.controller),
	});
	await postDownloadMessage({
		type: "stage-download",
		url,
		file,
		disposition: contentDispositionFor(name),
		contentType: file.type || "application/octet-stream",
	});
	debug("download-stage-done", {
		url,
		name,
		bytes: file.size || 0,
		contentType: file.type || "application/octet-stream",
	});
	return url;
}

// forgetStagedDownload expires unused staged responses after one minute.
function forgetStagedDownload(url, debug = () => {}) {
	setTimeout(() => {
		postDownloadMessage({ type: "forget-download", url }).catch((err) => {
			debug("download-forget-failed", {
				url,
				errorName: err?.name || "",
				errorMessage: err?.message || String(err),
			});
		});
	}, 60000);
}

// objectDownload prepares a blob: URL for clients that honor anchor filenames.
function objectDownload(file, name, publicHref = "") {
	const url = URL.createObjectURL(file);
	return {
		href: publicHref || url,
		clickHref: publicHref ? url : "",
		downloadName: name,
		useDownloadAttribute: true,
		cleanup: () => setTimeout(() => URL.revokeObjectURL(url), 1000),
	};
}

// prepareBlobDownload resolves the final href before the user taps the link.
export async function prepareBlobDownload(blob, name, shareID = "", options = {}) {
	const debug = typeof options.onDebug === "function" ? options.onDebug : () => {};
	const downloadName = safeDownloadName(name);
	const file = namedFile(blob, downloadName);
	const publicHref = shareID ? downloadURLPath(shareID, downloadName) : "";
	if (shareID && canStageDownload()) {
		try {
			const url = await stagedDownloadURL(file, shareID, downloadName, {
				onDebug: debug,
			});
			return {
				href: url,
				downloadName,
				useDownloadAttribute: false,
				cleanup: () => forgetStagedDownload(url, debug),
			};
		} catch (err) {
			debug("download-stage-failed", {
				name: downloadName,
				bytes: file.size || 0,
				contentType: file.type || "application/octet-stream",
				errorName: err?.name || "",
				errorMessage: err?.message || String(err),
			});
		}
	}
	debug("download-object-fallback", {
		name: downloadName,
		bytes: file.size || 0,
		contentType: file.type || "application/octet-stream",
		publicHref,
	});
	return objectDownload(file, downloadName, publicHref);
}
