import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import {
	canStageDownload,
	contentDispositionFor,
	downloadURLPath,
	safeDownloadName,
} from "../static/js/download.js";

function loadDownloadWorker() {
	const listeners = new Map();
	const worker = {
		location: new URL("https://example.test/"),
		clients: { claim: () => Promise.resolve() },
		skipWaiting: () => {},
		addEventListener: (type, listener) => listeners.set(type, listener),
	};
	const source = readFileSync(
		new URL("../static/js/download-sw.js", import.meta.url),
		"utf8",
	);
	new Function("self", source)(worker);
	return listeners;
}

function stageWorkerDownload(listeners, data) {
	let reply;
	listeners.get("message")({
		data: { type: "stage-download", ...data },
		ports: [{ postMessage: (value) => (reply = value) }],
	});
	expect(reply).toEqual({ ok: true });
}

function withDownloadGlobals(userAgent, fn) {
	const oldWindow = globalThis.window;
	const oldNavigator = globalThis.navigator;
	Object.defineProperty(globalThis, "window", {
		configurable: true,
		value: { isSecureContext: true },
	});
	Object.defineProperty(globalThis, "navigator", {
		configurable: true,
		value: { userAgent, serviceWorker: {} },
	});
	try {
		return fn();
	} finally {
		Object.defineProperty(globalThis, "window", {
			configurable: true,
			value: oldWindow,
		});
		Object.defineProperty(globalThis, "navigator", {
			configurable: true,
			value: oldNavigator,
		});
	}
}

async function fetchWorkerDownload(listeners, path) {
	let responsePromise;
	listeners.get("fetch")({
		request: new Request(`https://example.test${path}`),
		respondWith: (response) => (responsePromise = Promise.resolve(response)),
	});
	expect(responsePromise).toBeDefined();
	return await responsePromise;
}
describe("download helpers", () => {
	test("safeDownloadName keeps uploaded basename", () => {
		expect(safeDownloadName("notes.txt")).toBe("notes.txt");
		expect(safeDownloadName("dir/report.pdf")).toBe("report.pdf");
		expect(safeDownloadName("")).toBe("download");
		expect(safeDownloadName("bad\u0000name.bin")).toBe("bad_name.bin");
	});

	test("downloadURLPath keeps staged files under share URL", () => {
		expect(downloadURLPath("shareID", "dir/my file.bin")).toBe(
			"/s/shareID/f/my%20file.bin",
		);
	});

	test("content disposition includes ascii fallback and utf8 filename", () => {
		expect(contentDispositionFor("résumé 2026.pdf")).toBe(
			"attachment; filename=\"r_sum_ 2026.pdf\"; filename*=UTF-8''r%C3%A9sum%C3%A9%202026.pdf",
		);
	});
	test("download staging works on secure contexts including Android", () => {
		expect(
			withDownloadGlobals("Mozilla/5.0 (X11; Linux x86_64) Firefox/152", () =>
				canStageDownload(),
			),
		).toBe(true);
		expect(
			withDownloadGlobals("Mozilla/5.0 (Linux; Android 10) Chrome/149", () =>
				canStageDownload(),
			),
		).toBe(true);
	});
	test("staged service worker downloads are reusable until forgotten", async () => {
		const listeners = loadDownloadWorker();
		const url = downloadURLPath("shareID", "hello.txt");
		stageWorkerDownload(listeners, {
			url,
			file: new Blob(["hello"], { type: "text/plain" }),
			disposition: contentDispositionFor("hello.txt"),
			contentType: "text/plain",
		});

		const first = await fetchWorkerDownload(listeners, url);
		expect(first.status).toBe(200);
		expect(first.headers.get("Content-Disposition")).toContain(
			'filename="hello.txt"',
		);
		expect(await first.text()).toBe("hello");

		const second = await fetchWorkerDownload(listeners, url);
		expect(second.status).toBe(200);
		expect(await second.text()).toBe("hello");

		let reply;
		listeners.get("message")({
			data: { type: "forget-download", url },
			ports: [{ postMessage: (value) => (reply = value) }],
		});
		expect(reply).toEqual({ ok: true });

		const forgotten = await fetchWorkerDownload(listeners, url);
		expect(forgotten.status).toBe(404);
		expect(await forgotten.text()).toContain("download expired");
	});
	test("cache-busting query still resolves staged pathname key", async () => {
		const listeners = loadDownloadWorker();
		const url = downloadURLPath("shareID", "hello.txt");
		stageWorkerDownload(listeners, {
			url,
			file: new Blob(["hello"], { type: "text/plain" }),
			disposition: contentDispositionFor("hello.txt"),
			contentType: "text/plain",
		});

		// Per-click cache-busters append ?d=N so each tap is a distinct
		// navigation. The worker keys on pathname, so the query must not break
		// lookup.
		const res = await fetchWorkerDownload(listeners, `${url}?d=${Date.now()}`);
		expect(res.status).toBe(200);
		expect(res.headers.get("Content-Disposition")).toContain(
			'filename="hello.txt"',
		);
		expect(await res.text()).toBe("hello");
	});
});
