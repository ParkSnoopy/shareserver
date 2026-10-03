# Change Log

## 2026-10-03

### Fix
- Show completed upload links instead of hiding their rows because of empty decorative gutters.
- Render transfer phases immediately, let decrypt/unzip status paint before work, and retain completed mobile-readable progress.
- Reset failed-upload state on retry, prevent overlapping operations, and use stored ciphertext size when download headers omit the length.

### Update
- Remove the artificial delay before download responses while preserving persistent failed-password tracking, IP bans, and `Retry-After`.
- Set the installed version and Compose image tag to `v0.2.7`.

## 2026-09-07

### Add
- Add stateless encrypted-payload upload and password-gated download endpoints under `/api/v0`.
- Return Share URL, download URL, stored size, expiry, encryption owner, and cipher metadata from API uploads.
- Add `/api/` with a public archive list and API usage guide.
- Add a body-only private-key endpoint for listing matching active private Shares.
- Add root `robots.txt`, allowing `/api/*` while disallowing all other paths.

### Update
- Require every new Share payload to be encrypted before storage and protected by a separately hashed download-password verifier.
- Render initial HTML in the language selected for the browser session, while reusing the same catalogs for dynamic browser content.
- Set the installed version to `v0.2.0`; publish Docker images with matching version and `latest` tags.
- Derive browser authorization hashes before upload, download, and admin-login requests so plaintext passwords stay in the browser.
- Require SHA-256-derived `password_hash` credentials for archive uploads and downloads.

### Fix
- Remove anonymous payload GET access; wrong passwords return no payload bytes.
- Delay every download response by at least 2 seconds; persist a 23–29 hour IP ban after more than 10 failed password requests in one minute.
- Accept Railway's HTTPS forwarding header only from its documented internal proxy network.

### Breaking Changes
- Remove legacy Share blobs and metadata at startup when no download-password verifier exists; retaining them would consume storage while no safe authorization path exists.
- Remove plaintext and server-encrypted API upload modes; clients must encrypt payloads and submit only `password_hash` credentials.

## 2026-08-10

### Fix
- Render each upload Progress phase immediately before throttling later updates.
- Preserve download filenames on iPhone and iPad by avoiding WebKit's service-worker `.html` suffix path.
- Purge expired Share blobs and database rows before upload capacity checks so stale storage cannot cause false insufficient-space errors.
- Purge expired Shares during startup storage cleanup instead of waiting for the daily maintenance pass.
- Reconcile database rows and blob files automatically before archive and admin pages render, replacing the manual storage-cleanup control.

### Add
- Select individual or all Shares on `/admin/shares` and remove the selected blob/database pairs in one bulk action.

## 2026-07-12

### Update
- Deepen storage integrity into one module for Share removal, expiry Purge, and blob/database Reconcile.
- Deepen Render context with typed page data instead of caller-owned template maps.
- Render one production page in all configurations; remove Dev page content, routes, assets, and language strings.
- Require HTTPS for every Session cookie without configuring HSTS.
- Collapse Download action branching and lifecycle into `web/static/js/download.js`.

### Fix
- Report admin Share deletion success or failure instead of silently redirecting; preserve the database row when blob deletion fails so the operation can be retried.
- Verify admin deletion removes both the blob and Share database record.

## 2026-06-30

### Add
- "Download all as zip" button on share page when archive has more than one file: re-zips all opened entries client-side and stages through the same secure download path as individual files.
- `entriesToZip` helper in `web/static/js/zip.js` to re-package opened archive entries into a single zip blob.
- `share.downloadAll` i18n key in English, Chinese, and Korean catalogs.
- Share title exposed to client via `data-title` on the share control element for zip naming.
- Test coverage for `entriesToZip` round-trip and empty-list handling in `web/test/zip.test.mjs`.

### Fix
- Mobile sidebar now starts expanded on home/search pages and only collapses when a share detail (`/s/{id}`) is open; selecting a list entry still collapses it. Template no longer hardcodes `is-collapsed` so there is no flash before JS runs.
- Android (DuckDuckGo/Chrome) large single-blob downloads no longer hit "The webpage could not be displayed." The Android exclusion in `canStageDownload` was stale — the service worker already serves ArrayBuffer bodies (not Blob-backed streaming), which Android's download manager handles reliably. Android now uses the service-worker staging path like desktop, getting server-forced filenames and avoiding the blob: URL limit that blanked the page on large files.
- Large encrypted shares no longer crash the tab on memory-constrained mobile browsers (Android "The webpage could not be displayed" right after decrypt). The fetch now reads the stream directly into a single pre-sized `Uint8Array` instead of accumulating 5000+ chunk arrays and copying them into a second buffer; the bytes flow through decrypt (`decryptBlob` and `openArchive` both accept `Uint8Array` directly) into `unzipBytes` with no Blob→ArrayBuffer roundtrip. Ciphertext body and the fetched buffer are nulled as early as possible. On a 260MB encrypted share this cuts peak memory from ~780MB (chunk array + concat copy + Blob + decrypt copy) to ~260MB (one buffer, reused).

### Breaking Changes
- Removed the pure-JS noble crypto fallback. `crypto.subtle` is now the only decrypt/encrypt path. Encrypted shares opened over plain-HTTP (insecure context) show a clear "encryption is not supported on this browser" error instead of attempting a pure-JS AES-GCM decrypt that allocated hundreds of MB of JS heap and crashed memory-constrained mobile tabs. Serve the site over HTTPS or localhost so `crypto.subtle` is available. Deleted `web/static/vendor/noble/`, removed the `shareserver-env` dev meta tag, removed `state.loadingPureJS` i18n key.

## 2026-06-29

### Add
- Browser-language i18n catalogs in `web/static/i18n/*.json` with English fallback.
- Korean translation catalog for automatic page language.
- Download diagnostics for click dispatch, service-worker staging, prepared URL, response status, headers, bytes, fallback path.
- Debug output for silent catch paths: staged download forget, service-worker debug delivery, clipboard read.
- Test coverage for Android skipping service-worker download staging.
- ICO favicon generated from the ShareServer image at 64px and larger sizes.

### Update
- Template loader parses admin templates from `web/templates/admin/`.
- Page language follows browser language; user-facing picker removed.
- UI strings move behind keyed language files and `data-i18n` attributes.
- Debug logs: browser identity once at page ready; later actions keep compact context.
- Android downloads use gesture-time blob URLs instead of service-worker attachment responses.
- Base page template advertises the generated favicon from `web/static/img/`.

### Fix
- Download staging failures report safe error names/messages before fallback.
- Dev debug log no longer forces layout before page stylesheets finish loading.
- Initial page canvas starts dark before stylesheet bytes arrive.
- Mobile share search renders collapsed before JavaScript loads on every share page.
