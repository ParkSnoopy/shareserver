import { encryptBlob } from "./crypto.js";
import { fmtBytes, Progress } from "./progress.js";
import { initI18n, onLanguageChange, translate } from "./i18n.js";
import { canPreview, filesToZip } from "./zip.js";
import { settlePasswordInput } from "./ime.js";

await initI18n();

const form = document.getElementById("uploadForm");
const maxBytes = Number(form.dataset.maxBytes || 0);
const filesEl = document.getElementById("files");
const titleEl = form.elements.title;
const sourceEl = document.getElementById("source");
const passwordEl = form.elements.password;
const visibilityEl = document.getElementById("visibility");
const privateKeyLabel = document.getElementById("privateKeyLabel");
const privateKey = document.getElementById("privateKey");
const privateKeyRow = document.getElementById("privateKeyRow");
const fileSource = document.getElementById("fileSource");
const clipSource = document.getElementById("clipSource");
const progress = new Progress(document.getElementById("progress"));
const result = document.getElementById("result");
const clip = document.getElementById("clip");
const clipbox = document.getElementById("clipbox");
const clipCollapse = document.getElementById("clipCollapse");
const dropzone = document.getElementById("dropzone");
const fileList = document.getElementById("fileList");
let clipFiles = [];
let clipPreviewURLs = [];
let passwordComposing = false;
let selectedFiles = [];

function debugLog(event, data = {}) {
	console.info(`[shareserver:upload] ${event}`, data);
}

// clearClipboardPreviews removes old object URLs and preview nodes from clipboard mode.
function clearClipboardPreviews() {
	for (const url of clipPreviewURLs) URL.revokeObjectURL(url);
	clipPreviewURLs = [];
	for (const old of clipbox.querySelectorAll(".clip-preview")) old.remove();
}

const fileSize = document.getElementById("fileSize");

// syncSource switches visible controls between file picker and clipboard input.
function syncSource() {
	const clipMode = sourceEl.value === "clipboard";
	fileSource.hidden = clipMode;
	clipSource.hidden = !clipMode;
}

// syncVisibility shows private-key inputs only for private shares.
function syncVisibility() {
	const isPrivate = visibilityEl.value === "private";
	privateKeyRow.hidden = !isPrivate;
	privateKeyLabel.hidden = false;
	privateKey.hidden = false;
}

// basename turns a file path into the default share title without extension.
function basename(name) {
	return name
		.replace(/\\/g, "/")
		.split("/")
		.pop()
		.replace(/\.[^.]+$/, "");
}

// setDefaultTitle fills the title from the first selected file when the user left it blank.
function setDefaultTitle(files) {
	if (!titleEl.value && files.length) titleEl.value = basename(files[0].name);
}

// showClipboard renders media previews inside the editable clipbox without
// touching typed text: old previews are removed, the user's text stays, and
// new previews are appended after the text.
function showClipboard(files) {
	clipbox.classList.remove("clipbox-collapsed");
	clearClipboardPreviews();
	for (const file of files) {
		const wrap = document.createElement("div");
		wrap.className = "clip-preview";
		wrap.contentEditable = "false";
		wrap.append(previewClip(file));
		clipbox.append(wrap);
	}
	clipCollapse.hidden = files.length === 0;
	setDefaultTitle(files);
}

// previewClip renders an inline preview for a clipboard file: image/video/
// audio get native controls, pdf opens in an iframe, other previewable text
// shows as <pre>, and anything else shows name + size so the user still sees
// what was captured without dumping binary bytes into a text node.
function previewClip(file) {
	const mediaURL = () => {
		const url = URL.createObjectURL(file);
		clipPreviewURLs.push(url);
		return url;
	};
	const type = file.type || "";
	const name = file.name.toLowerCase();
	if (type.startsWith("image/") || /\.(png|jpe?g|gif|webp|svg)$/i.test(name)) {
		const img = new Image();
		img.src = mediaURL();
		img.alt = file.name;
		return img;
	}
	if (type.startsWith("video/") || /\.(mp4|webm|ogv)$/i.test(name)) {
		const v = document.createElement("video");
		v.src = mediaURL();
		v.controls = true;
		return v;
	}
	if (type.startsWith("audio/") || /\.(mp3|wav|weba)$/i.test(name)) {
		const a = document.createElement("audio");
		a.src = mediaURL();
		a.controls = true;
		return a;
	}
	if (type === "application/pdf" || name.endsWith(".pdf")) {
		const f = document.createElement("iframe");
		f.src = mediaURL();
		f.title = file.name;
		f.setAttribute("sandbox", "");
		return f;
	}
	if (
		canPreview(file.name, type) &&
		(type.startsWith("text/") || type === "application/json")
	) {
		const pre = document.createElement("pre");
		file.text().then((text) => {
			pre.textContent = text;
		});
		return pre;
	}
	const info = document.createElement("div");
	info.className = "clip-binary";
	info.textContent = `${file.name} · ${fmtBytes(file.size)}`;
	return info;
}

// updateFileSize renders selected file names and their combined size.
function updateFileSize(files) {
	const sum = files.reduce((acc, f) => acc + f.size, 0);
	fileSize.textContent = files.length
		? translate("upload.filesSummary", {
				bytes: fmtBytes(sum),
				count: files.length,
				noun: translate(
					files.length === 1 ? "upload.fileOne" : "upload.fileMany",
				),
			})
		: "";
	fileList.replaceChildren(
		...files.map((file) => {
			const row = document.createElement("div");
			row.className = "selected-file-row";
			const name = document.createElement("span");
			name.className = "selected-file-name";
			name.textContent = file.name;
			const meta = document.createElement("span");
			meta.className = "selected-file-meta";
			meta.textContent = fmtBytes(file.size);
			row.append(name, meta);
			return row;
		}),
	);
}

// setFiles mirrors drag/drop selections into the real file input and UI summary.
function setFiles(files) {
	selectedFiles = files;
	const dt = new DataTransfer();
	for (const file of files) dt.items.add(file);
	filesEl.files = dt.files;
	setDefaultTitle(files);
	updateFileSize(files);
}

sourceEl.addEventListener("change", syncSource);
visibilityEl.addEventListener("change", syncVisibility);
filesEl.addEventListener("change", () => {
	setFiles([...filesEl.files]);
});

for (const eventName of ["dragenter", "dragover"]) {
	dropzone.addEventListener(eventName, (event) => {
		event.preventDefault();
		dropzone.classList.add("drag-over");
	});
}

for (const eventName of ["dragleave", "drop"]) {
	dropzone.addEventListener(eventName, () => {
		dropzone.classList.remove("drag-over");
	});
}

dropzone.addEventListener("drop", (event) => {
	event.preventDefault();
	const files = [...event.dataTransfer.files];
	if (files.length) setFiles(files);
});
clipCollapse.addEventListener("click", () => {
	clipbox.classList.toggle("clipbox-collapsed");
	clipCollapse.textContent = clipbox.classList.contains("clipbox-collapsed")
		? translate("upload.expand")
		: translate("upload.collapse");
});

// mimeExt maps clipboard mime types to a real extension so non-image content
// (mp4, webm, mp3, pdf, ...) keeps a previewable, correctly-named file instead
// of being stamped clipboard-text-…txt.
const mimeExt = {
	"text/plain": "txt",
	"text/html": "html",
	"text/css": "css",
	"text/javascript": "js",
	"application/json": "json",
	"text/markdown": "md",
	"application/pdf": "pdf",
	"image/png": "png",
	"image/jpeg": "jpg",
	"image/gif": "gif",
	"image/webp": "webp",
	"image/svg+xml": "svg",
	"audio/mpeg": "mp3",
	"audio/wav": "wav",
	"audio/webm": "weba",
	"video/mp4": "mp4",
	"video/webm": "webm",
	"video/ogg": "ogv",
};
// clipboardName gives pasted binary content a timestamped previewable filename.
function clipboardName(type) {
	const stamp = new Date()
		.toISOString()
		.replace(/[-:]/g, "")
		.replace(/\.\d+Z$/, "Z");
	const ext = mimeExt[type] || (type.split("/")[1] || "bin").split("+")[0];
	const kind = type.startsWith("image/")
		? "image"
		: type.startsWith("video/")
			? "video"
			: type.startsWith("audio/")
				? "audio"
				: "content";
	return `clipboard-${kind}-${stamp}.${ext}`;
}

// bestType picks the one type to keep from a clipboard item: prefer real
// binary content (image/video/audio/pdf) over text, and text/plain over
// text/html, so a copied image doesn't also spawn an html sidecar and a
// copied paragraph doesn't produce two files.
function bestType(types) {
	const rank = (t) =>
		t.startsWith("image/") ||
		t.startsWith("video/") ||
		t.startsWith("audio/") ||
		t === "application/pdf"
			? 0
			: t === "text/plain"
				? 1
				: t.startsWith("text/")
					? 2
					: 3;
	return types.slice().sort((a, b) => rank(a) - rank(b))[0];
}

// insertTextAtCursor drops plain text into the editable clipbox at the caret,
// so "read clipboard" text becomes editable content (not a sealed file).
function insertTextAtCursor(text) {
	clipbox.focus();
	const sel = window.getSelection();
	if (!sel.rangeCount) {
		clipbox.append(document.createTextNode(text));
		return;
	}
	const range = sel.getRangeAt(0);
	if (!clipbox.contains(range.commonAncestorContainer)) {
		clipbox.append(document.createTextNode(text));
		return;
	}
	range.deleteContents();
	range.insertNode(document.createTextNode(text));
	range.collapse(false);
	sel.removeAllRanges();
	sel.addRange(range);
}

// clipText returns the user-typed text in the clipbox, excluding media
// previews. Uses the live (rendered) element's innerText so that <br> and
// <div> line breaks become \n (a detached clone falls back to textContent
// and loses them). Upload preserves typed text bytes exactly; preview-only
// normalization lives in share.js.
function clipText() {
	const previews = [...clipbox.querySelectorAll(".clip-preview, .clip-binary")];
	const marks = previews.map((p) => {
		const m = new Text("");
		p.replaceWith(m);
		return [m, p];
	});
	const text = (clipbox.innerText || "").trim();
	for (const [m, p] of marks) m.replaceWith(p);
	return text;
}

clip.onclick = async () => {
	clipFiles = [];
	try {
		if (navigator.clipboard?.read) {
			const items = await navigator.clipboard.read();
			for (const item of items) {
				if (!item.types.length) continue;
				const type = bestType(item.types);
				const blob = await item.getType(type);
				if (type === "text/plain" || type === "text/html") {
					// text clipboard content becomes editable text in the box,
					// not a sealed file — user can keep editing it.
					if (type === "text/plain") insertTextAtCursor(await blob.text());
					continue;
				}
				clipFiles.push(new File([blob], clipboardName(type), { type }));
			}
		} else if (navigator.clipboard?.readText) {
			const text = await navigator.clipboard.readText();
			if (text) insertTextAtCursor(text);
		}
		if (clipFiles.length) showClipboard(clipFiles);
		// no else: the box is always editable; placeholder guides when empty.
	} catch (err) {
		// clipboard blocked — leave the editable box as-is; user can type or paste.
		debugLog("clipboard-read-failed", {
			errorName: err?.name || "",
			errorMessage: err?.message || String(err),
		});
	}
};

// paste: only file items are captured as media (preventing the browser from
// inserting a broken file object). Text pastes natively into the editable box.
clipbox.addEventListener("paste", (event) => {
	const files = [...event.clipboardData.items]
		.filter((item) => item.kind === "file")
		.map((item) => item.getAsFile())
		.filter(Boolean);
	if (!files.length) return; // let text paste through natively
	event.preventDefault();
	clipFiles = files;
	showClipboard(clipFiles);
});

// uploadFormData sends the final archive with XHR upload progress callbacks.
function uploadFormData(out, size) {
	return new Promise((resolve, reject) => {
		const xhr = new XMLHttpRequest();
		xhr.open("POST", "/upload");
		const csrf = form.elements.csrf?.value || "";
		if (csrf) xhr.setRequestHeader("X-CSRF-Token", csrf);
		xhr.upload.onprogress = (event) => {
			const loaded = event.lengthComputable ? Math.min(event.loaded, size) : 0;
			progress.set("upload", loaded, size, translate("state.sending"));
		};
		xhr.onload = () => {
			if (xhr.status >= 200 && xhr.status < 300) {
				resolve(JSON.parse(xhr.responseText));
				return;
			}
			progress.set(
				"upload",
				size,
				size,
				`${translate("state.failed")} ${xhr.status}`,
			);
			reject(
				Error(
					translate("upload.uploadFailed", {
						status: xhr.status,
						message: xhr.responseText.trim() || xhr.statusText,
						size: fmtBytes(size),
					}),
				),
			);
		};
		xhr.onerror = () => reject(Error("upload network error"));
		xhr.send(out);
	});
}

form.onsubmit = async (event) => {
	event.preventDefault();
	result.textContent = "";
	await settlePasswordInput(passwordEl, () => passwordComposing);
	try {
		const fd = new FormData(form);
		let files =
			sourceEl.value === "clipboard" ? [...clipFiles] : [...filesEl.files];
		// clipboard mode: typed text in the box becomes a text file alongside
		// any pasted media, so both upload as separate entries in the zip.
		if (sourceEl.value === "clipboard") {
			const text = clipText();
			if (text) {
				const stamp = new Date()
					.toISOString()
					.replace(/[-:]/g, "")
					.replace(/\.\d+Z$/, "Z");
				files = [
					new File([text], `clipboard-text-${stamp}.txt`, {
						type: "text/plain",
					}),
					...files,
				];
			}
		}
		if (!files.length) throw Error(translate("upload.noFile"));
		if (!fd.get("title")) fd.set("title", basename(files[0].name));
		const inputSize = files.reduce((sum, file) => sum + file.size, 0);
		const stopZip = progress.pulse(
			"zip",
			inputSize,
			translate("state.working"),
		);
		let blob;
		let manifest;
		try {
			({ blob, manifest } = await filesToZip(files));
		} finally {
			stopZip();
		}
		progress.done("zip", inputSize);
		const zipSize = blob.size;
		let cipherMeta = "";
		const password = fd.get("password");
		if (password) {
			const stopEncrypt = progress.pulse(
				"encrypt",
				zipSize,
				translate("state.working"),
			);
			try {
				const enc = await encryptBlob(blob, password);
				blob = enc.blob;
				cipherMeta = JSON.stringify(enc.meta);
			} finally {
				stopEncrypt();
			}
			progress.done("encrypt", zipSize);
		}
		if (maxBytes && blob.size > maxBytes) {
			const msg = translate("upload.tooLarge", {
				actual: fmtBytes(blob.size),
				max: fmtBytes(maxBytes),
			});
			progress.fail("upload", msg, blob.size, maxBytes);
			throw Error(msg);
		}
		progress.set("upload", 0, zipSize, translate("state.sending"));
		const out = new FormData();
		for (const key of [
			"csrf",
			"title",
			"visibility",
			"private_key",
			"expiry_hours",
		])
			out.append(key, fd.get(key) || "");
		out.append("encrypted", password ? "1" : "0");
		out.append("cipher_meta", cipherMeta);
		out.append("zip_manifest", password ? "[]" : JSON.stringify(manifest));
		out.append("blob", blob, "share.blob");
		const json = await uploadFormData(out, zipSize);
		progress.done("upload", zipSize);
		result.innerHTML = `<a class="cmd" href="${json.url}">${location.origin}${json.url}</a><br><span class="muted">${translate("upload.resultMeta", { raw: fmtBytes(inputSize), zipped: fmtBytes(zipSize) })}</span>`;
	} catch (err) {
		const msg = err.message || String(err);
		if (
			!msg.startsWith("upload failed ") &&
			!msg.startsWith("upload too large ")
		) {
			progress.fail("upload", msg);
		}
		result.textContent = msg;
	}
};

if (passwordEl) {
	passwordEl.addEventListener("compositionstart", () => {
		passwordComposing = true;
	});
	passwordEl.addEventListener("compositionend", () => {
		passwordComposing = false;
	});
}

syncSource();
syncVisibility();
onLanguageChange(() => {
	updateFileSize(selectedFiles);
	if (clipbox.classList.contains("clipbox-collapsed")) {
		clipCollapse.textContent = translate("upload.expand");
	}
});
