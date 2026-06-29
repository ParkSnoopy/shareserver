const DOWNLOAD_PATH = /^\/s\/[^/]+\/f\/.+/;
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
		!DOWNLOAD_PATH.test(url.pathname)
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
	const key = downloadKey(data.url);
	downloads.set(key, {
		file: data.file,
		disposition: data.disposition,
		contentType: data.contentType || "application/octet-stream",
	});
	logDownloadDebug("download-sw-staged", {
		key,
		bytes: data.file?.size || 0,
		contentType: data.contentType || "application/octet-stream",
	});
}

// downloadResponse serves a staged download with filename headers until
// forgotten. The body is read as an ArrayBuffer because Android Chrome fails
// to stream Blob-backed responses for navigation downloads (first tap errors
// with "Network Error"); ArrayBuffer bodies are reliable across platforms.
async function downloadResponse(url) {
	const key = downloadKey(url);
	const staged = downloads.get(key);
	if (!staged) {
		logDownloadDebug("download-sw-response", {
			key,
			status: 404,
			error: "download expired",
		});
		return new Response("download expired\n", {
			status: 404,
			headers: { "Content-Type": "text/plain; charset=utf-8" },
		});
	}
	try {
		const body = await staged.file.arrayBuffer();
		logDownloadDebug("download-sw-response", {
			key,
			status: 200,
			bytes: body.byteLength,
			contentType: staged.contentType,
			disposition: staged.disposition,
		});
		return new Response(body, {
			headers: {
				"Cache-Control": "no-store",
				"Content-Disposition": staged.disposition,
				"Content-Type": staged.contentType,
			},
		});
	} catch (err) {
		logDownloadDebug("download-sw-response", {
			key,
			status: 500,
			errorName: err?.name || "",
			errorMessage: err?.message || String(err),
		});
		return new Response("download read failed\n", {
			status: 500,
			headers: { "Content-Type": "text/plain; charset=utf-8" },
		});
	}
}

function logDownloadDebug(event, data) {
	if (typeof self.clients?.matchAll !== "function") return;
	self.clients
		.matchAll({ type: "window", includeUncontrolled: true })
		.then((clients) => {
			for (const client of clients) {
				client.postMessage({ type: "shareserver-download-debug", event, data });
			}
		})
		.catch((err) => {
			console.warn("[shareserver:download-sw] debug delivery failed", {
				event,
				errorName: err?.name || "",
				errorMessage: err?.message || String(err),
			});
		});
}
