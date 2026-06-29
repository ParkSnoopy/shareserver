# Change Log

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
