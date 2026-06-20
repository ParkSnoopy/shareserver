import { unzip, zip } from "../vendor/fflate.mjs";
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
			type: f.type || "application/octet-stream",
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
export async function unzipBytes(buf) {
	const out = await new Promise((resolve, reject) => {
		unzip(new Uint8Array(buf), {}, (err, data) => {
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
