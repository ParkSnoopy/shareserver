# Change Log

## 2026-06-29

### Add
- Download diagnostics for click dispatch, service-worker staging, prepared URL, response status, headers, bytes, fallback path.
- Debug output for silent catch paths: staged download forget, service-worker debug delivery, clipboard read.

### Update
- Debug logs: browser identity once at page ready; later actions keep compact context.

### Fix
- Download staging failures report safe error names/messages before fallback.
