# Go without build base
FROM golang:1.26-alpine3.24 AS build

# Add build base (for CGO)
USER root
RUN apk add --no-cache build-base

# Build server binary
COPY . /build
WORKDIR /build
RUN CGO_ENABLED=1 go build -o shareserver ./cmd/shareserver

# Runner
FROM alpine:3.24

RUN apk add --no-cache bash ca-certificates
COPY --from=ghcr.io/techarohq/anubis:v1.27.0 /ko-app/anubis /app/anubis
ADD --chmod=644 https://raw.githubusercontent.com/TecharoHQ/anubis/v1.27.0/LICENSE /usr/share/licenses/anubis/LICENSE

# Add user `shareserver`
USER root
RUN \
	addgroup -S shareserver	&&\
	adduser -S -D -H -h /app -s /sbin/nologin -G shareserver shareserver

# Copy server binary
COPY --from=build /build/shareserver /app/shareserver

# Copy setup script and environment variables
COPY . /app

# Override ownership
RUN \
	chmod +x /app/entrypoint.sh	&&\
	mkdir -p /app/data/blobs	&&\
	chown -R shareserver:shareserver /app

# As user `shareserver`
USER shareserver
WORKDIR /app

ENV ANUBIS_DIFFICULTY=4
EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=5s --start-period=10s --retries=3 \
	CMD wget -q -O /dev/null http://127.0.0.1:9090/healthz && \
	    wget -q -O /dev/null http://127.0.0.1:8081/api/

# Run initial script
ENTRYPOINT ["/app/entrypoint.sh"]
