const DOWNLOAD_CACHE = "shareserver.downloads.v1";
const DOWNLOAD_PREFIX = "/__download__/";

self.addEventListener("install", () => {
	self.skipWaiting();
});

self.addEventListener("activate", (event) => {
	event.waitUntil(self.clients.claim());
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
	event.respondWith(downloadResponse(event.request));
});

// downloadResponse serves one staged download with filename headers, then evicts it.
async function downloadResponse(request) {
	const cache = await caches.open(DOWNLOAD_CACHE);
	const cached = await cache.match(request);
	if (!cached) {
		return new Response("download expired\n", {
			status: 404,
			headers: { "Content-Type": "text/plain; charset=utf-8" },
		});
	}
	await cache.delete(request);
	return cached;
}
