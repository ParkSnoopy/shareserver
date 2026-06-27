const DOWNLOAD_PREFIX = "/__download__/";
const downloads = new Map();

self.addEventListener("install", () => {
	self.skipWaiting();
});

self.addEventListener("activate", (event) => {
	event.waitUntil(self.clients.claim());
});

self.addEventListener("message", (event) => {
	const port = event.ports?.[0];
	const data = event.data || {};
	try {
		if (data.type === "stage-download") {
			stageDownload(data);
			port?.postMessage({ ok: true });
			return;
		}
		if (data.type === "forget-download") {
			downloads.delete(downloadKey(data.url));
			port?.postMessage({ ok: true });
			return;
		}
		port?.postMessage({ ok: false, error: "unknown download message" });
	} catch (err) {
		port?.postMessage({ ok: false, error: err.message || String(err) });
	}
});

self.addEventListener("fetch", (event) => {
	const url = new URL(event.request.url);
	if (
		event.request.method !== "GET" ||
		url.origin !== self.location.origin ||
		!url.pathname.startsWith(DOWNLOAD_PREFIX)
	) {
		return;
	}
	event.respondWith(downloadResponse(url));
});

// downloadKey normalizes staged URLs so window and worker agree on one key.
function downloadKey(url) {
	return new URL(url, self.location.origin).pathname;
}

// stageDownload stores one in-memory File/Blob with attachment headers.
function stageDownload(data) {
	if (!data.url || !data.file || !data.disposition) {
		throw Error("invalid staged download");
	}
	downloads.set(downloadKey(data.url), {
		file: data.file,
		disposition: data.disposition,
		contentType: data.contentType || "application/octet-stream",
	});
}

// downloadResponse serves one staged download with filename headers, then evicts it.
function downloadResponse(url) {
	const key = downloadKey(url);
	const staged = downloads.get(key);
	if (!staged) {
		return new Response("download expired\n", {
			status: 404,
			headers: { "Content-Type": "text/plain; charset=utf-8" },
		});
	}
	downloads.delete(key);
	return new Response(staged.file, {
		headers: {
			"Cache-Control": "no-store",
			"Content-Disposition": staged.disposition,
			"Content-Type": staged.contentType,
		},
	});
}
