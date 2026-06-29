import { ArchiveErrorCode, openArchive } from "./archive.js";
import { armDownloadAction } from "./download-action.js";
import { fmtBytes, Progress } from "./progress.js";
import { normalizeText } from "./text.js";
import { settlePasswordInput } from "./ime.js";
import { canPreview } from "./zip.js";

const root = document.getElementById("share");
const progress = new Progress(document.getElementById("progress"));
const listing = document.getElementById("listing");
const previewPane = document.getElementById("previewPane");
const loadBtn = document.getElementById("loadBtn");
const pass = document.getElementById("sharePassword");
const encrypted =
	root.dataset.encrypted === "true" || root.dataset.encrypted === "1";
function debugLog(event, data = {}) {
	const sink = globalThis.shareserverDecryptDebug;
	if (typeof sink === "function") sink(event, data);
}
let passwordComposing = false;
let entries = null;
let manifest = [];
let previewURL = "";
let openURL = "";

try {
	manifest = JSON.parse(root.dataset.manifest || "[]");
} catch (err) {
	debugLog("manifest-parse-failed", { error: err?.message || String(err) });
}
debugLog("page-ready", {
	shareID: root.dataset.id || "",
	encrypted,
	manifestEntries: manifest.length,
	userAgent: navigator.userAgent,
	platform: navigator.platform,
	vendor: navigator.vendor,
	secureContext: window.isSecureContext,
	subtleCrypto: Boolean(globalThis.crypto?.subtle),
	serviceWorkerController: Boolean(navigator.serviceWorker?.controller),
});
if (navigator.serviceWorker) {
	navigator.serviceWorker.addEventListener("message", (event) => {
		const message = event.data || {};
		if (message.type !== "shareserver-download-debug") return;
		debugLog(message.event || "download-worker-message", message.data || {});
	});
}

let activeRow = null;
let downloadedBlob = null;
let downloadCleanup = () => {};

// fetchBlobWithProgress downloads the stored archive while updating byte progress.
async function fetchBlobWithProgress(id, fallbackTotal) {
	const res = await fetch(`/blob/${id}`);
	debugLog("blob-response", {
		status: res.status,
		ok: res.ok,
		contentLength: res.headers.get("content-length") || "",
		contentType: res.headers.get("content-type") || "",
		fallbackTotal,
	});
	if (!res.ok) throw Error(`download failed ${res.status}`);
	const total = Number(res.headers.get("content-length") || fallbackTotal) || 0;
	const reader = res.body.getReader();
	const chunks = [];
	let loaded = 0;
	progress.set("download", 0, total, "fetching");
	while (true) {
		const { done, value } = await reader.read();
		if (done) break;
		chunks.push(value);
		loaded += value.length;
		progress.set("download", loaded, total, "fetching");
	}
	const all = new Uint8Array(loaded);
	let offset = 0;
	for (const chunk of chunks) {
		all.set(chunk, offset);
		offset += chunk.length;
	}
	debugLog("blob-fetched", { loaded, chunks: chunks.length });
	return new Blob([all]);
}

// load fetches the archive, opens it through the pure archive seam, and renders once.
async function load() {
	if (entries) {
		debugLog("load-skip-cached", { entries: entries.length });
		return entries;
	}
	const id = root.dataset.id;
	const fallbackTotal = manifest.find(Boolean)?.size || 0;
	debugLog("load-start", {
		shareID: id,
		encrypted,
		fallbackTotal,
		listingHidden: listing.hidden,
		previewHidden: previewPane.hidden,
	});
	if (!downloadedBlob) {
		downloadedBlob = await fetchBlobWithProgress(id, fallbackTotal);
	}
	progress.done("download", downloadedBlob.size);

	let cipher = {};
	if (encrypted) {
		cipher = JSON.parse(root.dataset.cipher || "{}");
	}
	const passwordValue = pass?.value || "";
	debugLog("open-archive", {
		encrypted,
		downloadedBlobSize: downloadedBlob.size,
	});

	let stopDecrypt = () => {};
	let stopUnzip = () => {};
	try {
		entries = await openArchive(downloadedBlob, {
			encrypted,
			password: passwordValue,
			cipher,
			manifest,
			onDecryptStart: (blob) => {
				stopDecrypt = progress.pulse("decrypt", blob.size, "working");
			},
			onDecryptDone: (blob) => {
				stopDecrypt();
				stopDecrypt = () => {};
				progress.done("decrypt", blob.size);
			},
			onDecryptDebug: (event, data) => {
				debugLog(event, data);
				if (event === "crypto-fallback-load") progress.state("decrypt", "loading pure JS");
				else if (event === "crypto-fallback") progress.state("decrypt", "working");
			},
			onUnzipStart: (blob) => {
				stopUnzip = progress.pulse("unzip", blob.size, "working");
			},
			onUnzipDone: (blob) => {
				stopUnzip();
				stopUnzip = () => {};
				progress.done("unzip", blob.size);
			},
		});
		debugLog("open-archive-done", { entries: entries.length });
	} finally {
		stopDecrypt();
		stopUnzip();
	}

	renderList();
	root.hidden = true;
	progress.reset();
	progress.el.hidden = true;
	return entries;
}

// renderList replaces the archive list with file rows and opens the first entry.
function renderList() {
	listing.hidden = false;
	previewPane.hidden = false;
	listing.innerHTML = "";
	const title = document.createElement("h3");
	title.textContent = "# Files";
	const list = document.createElement("div");
	list.className = "api-index-list";
	for (const entry of entries) list.append(rowFor(entry));
	listing.append(title, list);
	if (entries.length) openEntry(entries[0], list.firstElementChild);
	else showEmptyDetail("# archive empty.");
}

// rowFor builds one keyboard-clickable file row with previewability metadata.
function rowFor(entry) {
	const row = document.createElement("button");
	row.className = "api-index-row";
	const previewable = entry.previewable ?? canPreview(entry.name, entry.type);
	row.title = previewable ? "open preview" : "show file actions";
	row.onclick = () => openEntry(entry, row);

	const name = document.createElement("span");
	name.className = "archive-name";
	name.textContent = entry.name;
	const method = document.createElement("span");
	method.className = previewable
		? "method-label method-get"
		: "method-label method-put";
	method.textContent = previewable ? "GET" : "BIN";

	row.append(name, method);
	return row;
}

// typedBlob restores an entry MIME type before preview or download.
function typedBlob(entry) {
	return entry.type ? new Blob([entry.blob], { type: entry.type }) : entry.blob;
}

// entryPreviewURL builds a same-origin blob: URL from the in-browser entry
// blob. Blob URLs bypass X-Frame-Options / CSP frame-ancestors, so previews
// work for plain shares too (the server only exposes /blob/{id} for download;
// per-entry HTTP serving was removed to avoid stored same-origin XSS).
function entryPreviewURL(entry, forcedType = "") {
	const blob = forcedType
		? new Blob([entry.blob], { type: forcedType })
		: typedBlob(entry);
	return URL.createObjectURL(blob);
}

// unsafeRenderType identifies archive entries that would execute scripts (or
// run XSLT) if a browser rendered them as a top-level document. Opening such
// a file in a new window would give it the share site's origin, so the click
// handler forces text/plain to show source instead of executing it.
function unsafeRenderType(entry) {
	const type = (entry.type || "").toLowerCase();
	const name = entry.name.toLowerCase();
	return (
		type === "text/html" ||
		type === "image/svg+xml" ||
		type === "application/xml" ||
		type === "text/xml" ||
		name.endsWith(".html") ||
		name.endsWith(".htm") ||
		name.endsWith(".svg") ||
		name.endsWith(".xml") ||
		name.endsWith(".xhtml") ||
		name.endsWith(".xht")
	);
}
// clearPreview revokes old preview/download URLs and clears row selection state.
function clearPreview() {
	if (previewURL) URL.revokeObjectURL(previewURL);
	previewURL = "";
	if (openURL) URL.revokeObjectURL(openURL);
	openURL = "";
	downloadCleanup();
	downloadCleanup = () => {};
	if (activeRow) activeRow.classList.remove("active");
	activeRow = null;
}

// showEmptyDetail renders a neutral detail pane when no file can be selected.
function showEmptyDetail(message) {
	clearPreview();
	previewPane.replaceChildren(
		heading("# Archive"),
		comment(message || "# select a file."),
	);
}

// heading creates terminal-styled section titles for the detail pane.
function heading(text) {
	const h = document.createElement("h3");
	h.textContent = text;
	return h;
}

// comment creates muted explanatory text for empty and status states.
function comment(text) {
	const p = document.createElement("p");
	p.className = "comment-text";
	p.textContent = text;
	return p;
}


// openEntry selects a file, exposes download, and renders its preview area.
function openEntry(entry, row) {
	clearPreview();
	activeRow = row;
	row.classList.add("active");
	const actions = document.createElement("div");
	actions.className = "detail-actions";
	const download = document.createElement("a");
	download.className = "primary-action-button field-md";
	downloadCleanup = armDownloadAction(download, entry, root.dataset.id, {
		onDebug: debugLog,
	});
	actions.append(download);

	previewPane.replaceChildren(
		heading(`# ${entry.name}`),
		metaLine(entry),
		actions,
		previewWell(entry),
	);
}

// metaLine displays an entry's MIME type and byte size.
function metaLine(entry) {
	const meta = document.createElement("p");
	meta.className = "api-meta";
	const type = entry.type || "application/octet-stream";
	meta.textContent = `${type} · ${fmtBytes(entry.blob.size)}`;
	return meta;
}

// previewWell wraps one preview renderer in the shared detail styling and
// opens the entry in a fresh, wrapper-free browser window on click so the
// browser handles the file natively (full-size image, native PDF/video
// viewer, plain text). Native media controls stay usable: clicks on a
// <video>/<audio> element are left alone, and the well surface around them
// still opens the new window.
function previewWell(entry) {
	const well = document.createElement("div");
	well.className = "archive-preview preview-well";
	well.title = "Open in new window";
	well.append(previewFor(entry));
	well.addEventListener("click", (event) => {
		const t = event.target;
		if (t instanceof HTMLVideoElement || t instanceof HTMLAudioElement) return;
		// HTML/SVG/XML would execute with the share site's origin if a browser
		// rendered them as a top-level document, so force text/plain (source
		// view) for those and keep a dedicated openURL that clearPreview revokes.
		const force = unsafeRenderType(entry) ? "text/plain" : "";
		let url = force ? openURL : previewURL;
		if (!url) {
			url = entryPreviewURL(entry, force);
			if (force) openURL = url;
			else previewURL = url;
		}
		window.open(url, "_blank", "noopener");
	});
	return well;
}

// previewFor renders an inline preview from the entry's in-browser blob:
// image/video/audio get native elements, pdf opens in an iframe, text/code
// renders in a <pre>, and anything else shows a "no inline preview" note.
// All media/pdf previews use blob: URLs so they bypass the server's
// X-Frame-Options: DENY (which blocks iframing the HTTP file URL).
function previewFor(entry) {
	const type = entry.type || "";
	const name = entry.name.toLowerCase();
	if (type.startsWith("image/") || /\.(png|jpe?g|gif|webp|svg)$/i.test(name)) {
		const img = new Image();
		previewURL = entryPreviewURL(entry);
		img.src = previewURL;
		img.alt = entry.name;
		return img;
	}
	if (type.startsWith("video/") || /\.(mp4|webm|ogv)$/i.test(name)) {
		const video = document.createElement("video");
		previewURL = entryPreviewURL(entry);
		video.src = previewURL;
		video.controls = true;
		return video;
	}
	if (type.startsWith("audio/") || /\.(mp3|wav|weba)$/i.test(name)) {
		const audio = document.createElement("audio");
		previewURL = entryPreviewURL(entry);
		audio.src = previewURL;
		audio.controls = true;
		return audio;
	}
	if (type === "application/pdf" || name.endsWith(".pdf")) {
		const frame = document.createElement("iframe");
		previewURL = entryPreviewURL(entry, "application/pdf");
		frame.src = previewURL;
		frame.title = entry.name;
		frame.setAttribute("sandbox", "");
		return frame;
	}
	if (canPreview(entry.name, type)) {
		// text/code: render directly from the blob, no iframe, no URL to revoke.
		const pre = document.createElement("pre");
		pre.className = "text-preview";
		entry.blob.text().then((text) => {
			pre.textContent = normalizeText(text);
		});
		return pre;
	}
	const note = document.createElement("div");
	note.className = "muted";
	note.textContent = "no inline preview. use download.";
	return note;
}

// loadEntries resets progress and reports list/decrypt errors without stale output.
async function loadEntries() {
	await settlePasswordInput(pass, () => passwordComposing, { onDebug: debugLog });
	progress.reset();
	entries = null;
	try {
		await load();
	} catch (err) {
		debugLog("load-failed", {
			errorName: err?.name || "",
			errorMessage: err?.message || String(err),
			errorCode: err?.code || "",
			phase: failurePhase(err),
			causeName: err?.cause?.name || "",
			causeMessage: err?.cause?.message || "",
		});
		progress.fail(failurePhase(err), err.message || "archive open failed");
	}
}

// failurePhase keeps password errors under decrypt and archive errors under list.
function failurePhase(err) {
	if (
		err?.code === ArchiveErrorCode.PasswordRequired ||
		err?.code === ArchiveErrorCode.WrongPassword ||
		err?.code === ArchiveErrorCode.UnsupportedCrypto
	) {
		return "decrypt";
	}
	if (err?.code === ArchiveErrorCode.CorruptArchive) return "list";
	return encrypted ? "decrypt" : "list";
}

loadBtn.onclick = loadEntries;
if (pass) {
	pass.addEventListener("compositionstart", () => {
		debugLog("password-composition-start");
		passwordComposing = true;
	});
	pass.addEventListener("compositionend", () => {
		passwordComposing = false;
		debugLog("password-composition-end");
	});
	pass.addEventListener("keydown", (event) => {
		if (event.key === "Enter") {
			event.preventDefault();
			debugLog("password-enter", {
				isComposing: event.isComposing,
				passwordComposing,
			});
			if (!event.isComposing && !passwordComposing) loadEntries();
		}
	});
}
