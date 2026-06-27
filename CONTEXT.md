# shareserver

A file-sharing service: upload an archive (optionally encrypted), get a shareable link with optional expiry and visibility. The browser zips and encrypts; the server stores opaque bytes and metadata.

## Language

**Share**:
An uploaded archive with metadata: visibility, optional expiry, optional encryption, and a link to its stored blob.
_Avoid_: post, item, entry (reserved for archive contents)

**Blob**:
The opaque stored bytes of a Share — a zip (plain) or encrypted zip. The server never decrypts; only the browser does.
_Avoid_: file, payload, content

**Archive**:
The decrypted zip and its entries, as seen by the browser after opening a Share.
_Avoid_: package, bundle

**Entry**:
One file inside an Archive, with its name, MIME type, blob, and previewability.
_Avoid_: item, record, row

**Session**:
A browser session — anonymous by default, elevated to admin on login. Carries a CSRF token.
_Avoid_: cookie, token (too narrow)

**Purge**:
The act of deleting a Share's blob and metadata after its expiry passes the grace window.
_Avoid_: cleanup (reserved for session cleanup), delete (too generic)

**Grace window**:
The retention period after expiry before a Share becomes purgeable. Currently 24 hours.
_Avoid_: buffer, delay

**Active**:
A Share that is non-purged and not expired at a given instant. The canonical instant is stamped once per request by the clock middleware.
_Avoid_: live, valid, current

**Expiry rule**:
The single definition of whether a Share is expired, active, or purgeable at one instant. Owns the grace window. The SQL predicate is an adapter of this rule.
_Avoid_: status check, validation

**Reconcile**:
Storage repair: comparing registered blobs against disk and removing orphans.
_Avoid_: sync, repair

**Upload**:
The intake flow: validate → enforce storage cap → store blob → insert metadata → audit. Owned by the Upload module.
_Avoid_: submit, create

**Render**:
Template rendering with CSRF, admin, and dev context. Owned by the Render module.
_Avoid_: view, response (too generic)

**Download action**:
The per-entry download orchestration: secure-context service-worker staging vs insecure-context blob URL fallback. Owned by the Download action module.
_Avoid_: download handler, click handler (too narrow)
