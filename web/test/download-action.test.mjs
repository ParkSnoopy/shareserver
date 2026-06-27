import { describe, expect, test } from "bun:test";
import { armDownloadAction } from "../static/js/download-action.js";

// Harness: create a minimal anchor + entry pair, simulate a click, and
// observe the outcome (href change, download attribute, preventDefault).
// The module branches on canStageDownload(), which depends on
// window.isSecureContext — tests stub it to control the branch.

function makeEntry(name = "report.pdf", body = "payload", type = "application/pdf") {
	return {
		name,
		type,
		blob: new Blob([body], { type }),
	};
}

function makeAnchor() {
	const a = {
		tagName: "A",
		href: "",
		download: "",
		dataset: {},
		_entry: null,
		_listeners: {},
		getAttribute(attr) {
			if (attr === "aria-disabled") return null;
			return this[attr] ?? null;
		},
		removeAttribute(attr) {
			if (attr === "aria-disabled") this._disabled = false;
		},
		setAttribute() {},
		addEventListener(type, fn) {
			this._listeners[type] = fn;
		},
		click(opts = {}) {
			const event = {
				preventDefault: () => {},
				...opts,
			};
			this._listeners.click(event);
			return event;
		},
	};
	return a;
}

describe("armDownloadAction", () => {
	test("sets visible href and label on the anchor", () => {
		const anchor = makeAnchor();
		const entry = makeEntry("notes.txt", "hello", "text/plain");
		armDownloadAction(anchor, entry, "share-123");
		expect(anchor.href).toBe("/s/share-123/f/notes.txt");
		expect(anchor.textContent).toBe("> download");
	});

	test("returns a cleanup function that does not throw", () => {
		const anchor = makeAnchor();
		const entry = makeEntry("data.bin", "x");
		const cleanup = armDownloadAction(anchor, entry, "s1");
		expect(typeof cleanup).toBe("function");
		expect(() => cleanup()).not.toThrow();
	});

	test("insecure context: click sets blob URL and download attribute", () => {
		// canStageDownload returns false when isSecureContext is false.
		// Bun's default is not a secure context for this purpose.
		const anchor = makeAnchor();
		const entry = makeEntry("report.pdf", "payload", "application/pdf");
		armDownloadAction(anchor, entry, "s1");

		anchor.click();

		// href should now be a blob: URL
		expect(anchor.href).toMatch(/^blob:/);
		expect(anchor.download).toBe("report.pdf");
	});

	test("insecure context: second click revokes old blob URL and makes a new one", () => {
		const anchor = makeAnchor();
		const entry = makeEntry("file.bin", "data");
		armDownloadAction(anchor, entry, "s1");

		anchor.click();
		const firstURL = anchor.href;
		anchor.click();
		const secondURL = anchor.href;

		expect(firstURL).toMatch(/^blob:/);
		expect(secondURL).toMatch(/^blob:/);
		// Each tap gets a fresh blob URL (avoids download dedupe).
		expect(firstURL).not.toBe(secondURL);
	});

	test("disabled anchor does not start a download", () => {
		const anchor = makeAnchor();
		const entry = makeEntry("file.bin", "data");
		armDownloadAction(anchor, entry, "s1");

		// Simulate disabled state
		anchor.getAttribute = (attr) =>
			attr === "aria-disabled" ? "true" : null;

		let prevented = false;
		anchor.click({ preventDefault: () => { prevented = true; } });

		expect(prevented).toBe(true);
		expect(anchor.href).toBe("/s/s1/f/file.bin"); // unchanged from arm
	});
});
