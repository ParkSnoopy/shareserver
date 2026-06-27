// Download action: owns the secure/insecure download branching and blob URL
// lifecycle for archive entry downloads.
//
// On a secure context (HTTPS/localhost): re-stages the in-browser entry
// through the service worker on every tap so repeated downloads never depend
// on stale worker memory. The worker serves Content-Disposition, so the
// filename is server-forced.
//
// On an insecure context (plain-HTTP LAN): no service worker, so build a
// fresh blob: URL synchronously within the user gesture so Android Chrome
// honors the download attribute and saves the uploaded filename. A fresh
// blob URL per tap avoids same-URL download dedupe.

import {
	canStageDownload,
	clickPreparedDownload,
	downloadURLPath,
	prepareBlobDownload,
	safeDownloadName,
} from "./download.js";

// armDownloadAction wires a download anchor's click handler and visible href.
// The anchor's href stays under /s/{shareID}/f/{filename} for link semantics;
// the click handler intercepts and routes through staging or blob fallback.
// Returns a cleanup function that revokes any held blob URL.
export function armDownloadAction(anchor, entry, shareID) {
	anchor._entry = entry;
	anchor.href = downloadURLPath(shareID, entry.name);
	anchor.textContent = "> download";
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
			return;
		}
		if (canStageDownload()) {
			event.preventDefault();
			if (anchor.dataset.busy === "1") return;
			anchor.dataset.busy = "1";
			prepareBlobDownload(typedBlob(e), e.name, shareID)
				.then((prepared) => {
					clickPreparedDownload(prepared);
					prepared.cleanup();
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
