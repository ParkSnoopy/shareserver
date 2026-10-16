# shareserver

A small, terminal-style file share web app in Go with server-rendered pages and local assets.  
Clients ZIP and encrypt payloads before upload; the server stores opaque ciphertext and metadata through Ent on SQLite.  
Shares support public or unlisted discovery, password-gated downloads, expiry, and administrative management.  

## API

| Method | Route | Interface |
| --- | --- | --- |
| `GET` | `/api/` | HTML public archive list and concise API contract |
| `POST` | `/api/v0/upload` | Ordered multipart upload of a client-encrypted ZIP |
| `POST` | `/api/v0/list` | Body-only private archive discovery |
| `POST` | `/api/v0/download/{uuid}` | Password-hash-authorized ciphertext response |

See the [API reference](./docs/LLM_WIKI.md#public-route-inventory) for transport, authorization, payload formats, limits, and retry behavior.  

## Development

Development requires the Go toolchain declared in [go.mod](./go.mod), a C toolchain for SQLite/cgo, and Bun for client tests.  
Run these commands from the repository root.  

```sh
go build -o shareserver ./cmd/shareserver
go test ./...
bun test
```

## Documentation

- [Development and deployment](./docs/DEVELOPMENT.md): security invariants, container and native setup, configuration, HTTPS, proxy trust, and integration tests.
- [Architecture and API reference](./docs/LLM_WIKI.md): module ownership, protocol contracts, persistence, and agent instructions.
- [Release history](./docs/CHANGELOG.md).
