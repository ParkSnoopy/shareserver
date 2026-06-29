// Download action: owns the secure/insecure download branching and blob URL
// lifecycle for archive entry downloads.
//
// On a secure context (HTTPS/localhost): re-stages the in-browser entry
// through the service worker on every tap so repeated downloads never depend
// on stale worker memory. The worker serves Content-Disposition with an
// ArrayBuffer body, so the filename is server-forced and large blobs download
// reliably across platforms including Android.
//
// On insecure contexts (plain-HTTP LAN): build a fresh blob: URL synchronously
// within the user gesture so browsers honor the download attribute and save
// the uploaded filename. A fresh blob URL per tap avoids same-URL download
// dedupe.

import {
	canStageDownload,
	clickPreparedDownload,
	downloadURLPath,
	prepareBlobDownload,
	safeDownloadName,
} from "./download.js";
import { translate } from "./i18n.js";

// armDownloadAction wires a download anchor's click handler and visible href.
// The anchor's href stays under /s/{shareID}/f/{filename} for link semantics;
// the click handler intercepts and routes through staging or blob fallback.
// Returns a cleanup function that revokes any held blob URL.
export function armDownloadAction(anchor, entry, shareID, options = {}) {
	const debug =
		typeof options.onDebug === "function" ? options.onDebug : () => {};
	anchor._entry = entry;
	anchor.href = downloadURLPath(shareID, entry.name);
	anchor.textContent = translate("action.download");
	anchor.removeAttribute("aria-disabled");

	let blobURL = "";

	anchor.addEventListener("click", (event) => {
		if (anchor.getAttribute("aria-disabled") === "true") {
			event.preventDefault();
			return;
		}
		const e = anchor._entry;
		if (!e) {
			event.preventDefault();
			debug("download-click-missing-entry", { shareID });
			return;
		}
		debug("download-click", downloadDebugData(e, shareID));
		if (canStageDownload()) {
			event.preventDefault();
			if (anchor.dataset.busy === "1") return;
			anchor.dataset.busy = "1";
			prepareBlobDownload(typedBlob(e), e.name, shareID, { onDebug: debug })
				.then((prepared) => {
					debug("download-prepared", {
						...downloadDebugData(e, shareID),
						href: prepared.href || "",
						clickHrefScheme: urlScheme(
							prepared.clickHref || prepared.href || "",
						),
						useDownloadAttribute: prepared.useDownloadAttribute,
					});
					clickPreparedDownload(prepared);
					debug("download-click-dispatched", {
						...downloadDebugData(e, shareID),
						href: prepared.href || "",
						clickHrefScheme: urlScheme(
							prepared.clickHref || prepared.href || "",
						),
						useDownloadAttribute: prepared.useDownloadAttribute,
					});
					prepared.cleanup();
				})
				.catch((err) => {
					debug("download-prepare-failed", {
						...downloadDebugData(e, shareID),
						errorName: err?.name || "",
						errorMessage: err?.message || String(err),
					});
				})
				.finally(() => {
					delete anchor.dataset.busy;
				});
			return;
		}
		// Insecure context: fresh blob URL per tap within the user gesture.
		if (blobURL) URL.revokeObjectURL(blobURL);
		blobURL = URL.createObjectURL(typedBlob(e));
		anchor.href = blobURL;
		anchor.download = safeDownloadName(e.name);
		debug("download-object-url-ready", {
			...downloadDebugData(e, shareID),
			clickHrefScheme: "blob",
		});
	});

	return () => {
		if (blobURL) URL.revokeObjectURL(blobURL);
		blobURL = "";
	};
}

// typedBlob restores an entry MIME type before staging or blob URL creation.
function typedBlob(entry) {
	return entry.type ? new Blob([entry.blob], { type: entry.type }) : entry.blob;
}

function downloadDebugData(entry, shareID) {
	return {
		shareID,
		name: safeDownloadName(entry.name),
		contentType: entry.type || "",
		bytes: entry.blob?.size || 0,
		secureContext: globalThis.window?.isSecureContext,
		serviceWorkerController: Boolean(
			globalThis.navigator?.serviceWorker?.controller,
		),
	};
}

function urlScheme(url) {
	const match = String(url || "").match(/^([a-z][a-z0-9+.-]*):/i);
	return match ? match[1] : "same-origin";
}
