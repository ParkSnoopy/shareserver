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

EXPOSE 8080

# Run initial script
ENTRYPOINT ["/app/entrypoint.sh"]
