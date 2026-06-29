import { unzip, zip } from "../vendor/fflate.mjs";

const mimeByExt = {
	txt: "text/plain",
	md: "text/markdown",
	csv: "text/csv",
	log: "text/plain",
	html: "text/html",
	htm: "text/html",
	css: "text/css",
	js: "text/javascript",
	mjs: "text/javascript",
	json: "application/json",
	pdf: "application/pdf",
	png: "image/png",
	jpg: "image/jpeg",
	jpeg: "image/jpeg",
	gif: "image/gif",
	webp: "image/webp",
	svg: "image/svg+xml",
	mp3: "audio/mpeg",
	wav: "audio/wav",
	weba: "audio/webm",
	mp4: "video/mp4",
	webm: "video/webm",
	ogv: "video/ogg",
};

// mimeFromName maps known filename extensions to preview/download MIME types.
export function mimeFromName(name) {
	const ext = (name || "").toLowerCase().split(".").pop();
	return mimeByExt[ext] || "";
}

// safeName keeps zip entry paths relative and unique.
function safeName(name, seen) {
	name = (name || "file")
		.replace(/\\/g, "/")
		.split("/")
		.filter((p) => p && p !== "." && p !== "..")
		.join("/");
	if (!name) name = "file";
	if (name.startsWith("/")) name = name.slice(1);
	let base = name,
		i = 2;
	while (seen.has(name)) {
		const dot = base.lastIndexOf(".");
		name =
			dot > 0
				? `${base.slice(0, dot)} (${i++})${base.slice(dot)}`
				: `${base} (${i++})`;
	}
	seen.add(name);
	return name;
}
// filesToZip compresses browser-selected files and records safe preview metadata.
export async function filesToZip(files) {
	const seen = new Set(),
		input = {},
		manifest = [];
	for (const f of files) {
		const name = safeName(f.webkitRelativePath || f.name, seen);
		const buf = new Uint8Array(await f.arrayBuffer());
		input[name] = buf;
		manifest.push({
			name,
			size: f.size,
			type: f.type || mimeFromName(name) || "application/octet-stream",
			mtime: f.lastModified || Date.now(),
		});
	}
	const zipped = await new Promise((resolve, reject) => {
		zip(input, { level: 6 }, (err, data) => {
			if (err) reject(err);
			else resolve(data);
		});
	});
	return { blob: new Blob([zipped], { type: "application/zip" }), manifest };
}
// unzipBytes expands an archive into named browser blobs for preview and download.
export async function unzipBytes(buf) {
	const input = buf instanceof Uint8Array ? buf : new Uint8Array(buf);
	const out = await new Promise((resolve, reject) => {
		unzip(input, {}, (err, data) => {
			if (err) reject(err);
			else resolve(data);
		});
	});
	return Object.entries(out).map(([name, bytes]) => ({
		name,
		bytes,
		blob: new Blob([bytes]),
		size: bytes.byteLength,
	}));
}
// entriesToZip re-packages opened archive entries into a single zip blob for
// "download all" — each entry's raw bytes is re-compressed at level 6.
export async function entriesToZip(entries) {
	const seen = new Set();
	const input = {};
	for (const entry of entries) {
		const name = safeName(entry.name, seen);
		input[name] = entry.bytes || new Uint8Array(await entry.blob.arrayBuffer());
	}
	const zipped = await new Promise((resolve, reject) => {
		zip(input, { level: 6 }, (err, data) => {
			if (err) reject(err);
			else resolve(data);
		});
	});
	return new Blob([zipped], { type: "application/zip" });
}
// canPreview decides whether an entry can render inline instead of download-only.
export function canPreview(name, type = "") {
	const ext = name.toLowerCase().split(".").pop();
	return (
		type.startsWith("text/") ||
		type.startsWith("image/") ||
		type === "application/pdf" ||
		type.startsWith("audio/") ||
		type.startsWith("video/") ||
		[
			"txt",
			"md",
			"json",
			"csv",
			"log",
			"html",
			"css",
			"js",
			"go",
			"py",
			"pdf",
			"png",
			"jpg",
			"jpeg",
			"gif",
			"webp",
			"svg",
			"mp3",
			"wav",
			"mp4",
			"webm",
		].includes(ext)
	);
}
