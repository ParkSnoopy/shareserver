import { decryptBlob } from "./crypto.js";
import { fmtBytes, Progress } from "./progress.js";
import { canPreview, mimeFromName, unzipBytes } from "./zip.js";
import { normalizeText } from "./text.js";

const root = document.getElementById("share");
const progress = new Progress(document.getElementById("progress"));
const listing = document.getElementById("listing");
const previewPane = document.getElementById("previewPane");
const loadBtn = document.getElementById("loadBtn");
const pass = document.getElementById("password");
const encrypted =
	root.dataset.encrypted === "true" || root.dataset.encrypted === "1";
let entries = null;
let manifest = [];
let previewURL = "";

try {
	manifest = JSON.parse(root.dataset.manifest || "[]");
} catch {}

let activeRow = null;
let downloadedBlob = null;

async function fetchBlobWithProgress(id, fallbackTotal) {
	const res = await fetch(`/blob/${id}`);
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
	return new Blob([all]);
}

async function load() {
	if (entries) return entries;
	const id = root.dataset.id;
	if (!downloadedBlob) {
		downloadedBlob = await fetchBlobWithProgress(
			id,
			(manifest.find(Boolean) || {}).size || 0,
		);
	}
	progress.done("download", downloadedBlob.size);
	let blob = downloadedBlob;
	if (encrypted) {
		if (!pass.value) throw Error("password required");
		const stopDecrypt = progress.pulse("decrypt", blob.size, "working");
		try {
			blob = await decryptBlob(
				blob,
				pass.value,
				JSON.parse(root.dataset.cipher || "{}"),
			);
		} finally {
			stopDecrypt();
		}
		progress.done("decrypt", blob.size);
	}
	const stopUnzip = progress.pulse("unzip", blob.size, "working");
	let raw;
	try {
		raw = await unzipBytes(await blob.arrayBuffer());
	} finally {
		stopUnzip();
	}
	entries = raw.map((entry) => ({
		...entry,
		type: typeForEntry(entry),
	}));
	progress.done("unzip", blob.size);
	renderList();
	root.hidden = true;
	progress.reset();
	progress.el.hidden = true;
	return entries;
}

function renderList() {
	listing.innerHTML = "";
	const title = document.createElement("h3");
	title.textContent = "# files";
	const list = document.createElement("div");
	list.className = "api-index-list";
	for (const entry of entries) list.append(rowFor(entry));
	listing.append(title, list);
	if (entries.length) openEntry(entries[0], list.firstElementChild);
	else showEmptyDetail("# archive empty.");
}

function rowFor(entry) {
	const row = document.createElement("button");
	row.className = "api-index-row";
	const previewable = canPreview(entry.name, entry.type);
	row.title = previewable ? "open preview" : "show file actions";
	row.onclick = () => openEntry(entry, row);

	const name = document.createElement("span");
	name.className = "archive-name";
	name.textContent = entry.name;
	const method = document.createElement("span");
	method.className = previewable ? "method-label method-get" : "method-label method-put";
	method.textContent = previewable ? "GET" : "BIN";

	row.append(name, method);
	return row;
}

function typedBlob(entry) {
	return entry.type ? new Blob([entry.blob], { type: entry.type }) : entry.blob;
}

function typeForEntry(entry) {
	const manifestType = (manifest.find((item) => item.name === entry.name) || {}).type || "";
	return manifestType && manifestType !== "application/octet-stream"
		? manifestType
		: mimeFromName(entry.name) || manifestType;
}


// entryPreviewURL builds a same-origin blob: URL from the in-browser entry
// blob. Blob URLs bypass X-Frame-Options / CSP frame-ancestors, so previews
// work for plain shares too (the server only exposes /blob/{id} for download;
// per-entry HTTP serving was removed to avoid stored same-origin XSS).
function entryPreviewURL(entry, forcedType = "") {
	const blob = forcedType ? new Blob([entry.blob], { type: forcedType }) : typedBlob(entry);
	return URL.createObjectURL(blob);
}

function clearPreview() {
	if (previewURL) URL.revokeObjectURL(previewURL);
	previewURL = "";
	if (activeRow) activeRow.classList.remove("active");
	activeRow = null;
}

function showEmptyDetail(message) {
	clearPreview();
	previewPane.replaceChildren(
		heading("# archive"),
		comment(message || "# select a file."),
	);
}

function heading(text) {
	const h = document.createElement("h3");
	h.textContent = text;
	return h;
}

function comment(text) {
	const p = document.createElement("p");
	p.className = "comment-text";
	p.textContent = text;
	return p;
}

function openEntry(entry, row) {
	clearPreview();
	activeRow = row;
	row.classList.add("active");
	const actions = document.createElement("div");
	actions.className = "detail-actions";
	const download = document.createElement("button");
	download.className = "primary-action-button field-md";
	download.textContent = "> download";
	download.onclick = () => save(typedBlob(entry), entry.name);
	actions.append(download);

	previewPane.replaceChildren(
		heading(`# ${entry.name}`),
		metaLine(entry),
		actions,
		previewWell(entry),
	);
}

function metaLine(entry) {
	const meta = document.createElement("p");
	meta.className = "api-meta";
	const type = entry.type || "application/octet-stream";
	meta.textContent = `${type} · ${fmtBytes(entry.blob.size)}`;
	return meta;
}

function previewWell(entry) {
	const well = document.createElement("div");
	well.className = "archive-preview preview-well";
	well.append(previewFor(entry));
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

function save(blob, name) {
	const a = document.createElement("a");
	a.href = URL.createObjectURL(blob);
	a.download = name;
	document.body.append(a);
	a.click();
	setTimeout(() => {
		URL.revokeObjectURL(a.href);
		a.remove();
	}, 1000);
}

async function loadEntries() {
	progress.reset();
	entries = null;
	try {
		await load();
	} catch (err) {
		progress.fail(
			encrypted ? "decrypt" : "list",
			err.message || "wrong password. nothing decrypted.",
		);
	}
}

loadBtn.onclick = loadEntries;
if (pass) {
	pass.addEventListener("keydown", (event) => {
		if (event.key === "Enter") {
			event.preventDefault();
			loadEntries();
		}
	});
}

