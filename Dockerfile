# syntax=docker/dockerfile:1
# Production image for dokku: the API server, the migrate command, which app.json's predeploy step runs, the
# check-links command, which app.json's cron entry runs daily (docs/DEPLOY.md, "Link checks (cron)"), and the
# one-off import-mediawiki command (docs/DEPLOY.md, "Importing the legacy wiki").
FROM golang:1.27.3-alpine AS builder

# Build with the image's Go, which is the version go.mod requires, rather than downloading a toolchain.
ENV GOTOOLCHAIN=local CGO_ENABLED=0

WORKDIR /src

COPY . .
# go build downloads only the modules the commands import, not the tools go.mod also requires; the cache
# mounts keep those modules and the build cache across builds. -trimpath drops local paths from the binaries.
# Without cgo they are static, so they run on plain alpine.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath -o /out/bardic-torch ./cmd/app && \
    go build -trimpath -o /out/migrate ./cmd/migrate && \
    go build -trimpath -o /out/import-mediawiki ./cmd/import-mediawiki && \
    go build -trimpath -o /out/check-links ./cmd/check-links

FROM alpine:3.24

# ca-certificates for TLS to Auth0's JWKS endpoint, to the links check-links checks and to the Wayback Machine;
# tzdata so time zones resolve inside the container.
RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S -g 10001 bardic && \
    adduser -S -D -H -u 10001 -G bardic bardic

WORKDIR /app
COPY --from=builder /out/bardic-torch /out/migrate /out/import-mediawiki /out/check-links /app/
# dokku reads Procfile and app.json from the repository; older versions read them from WORKDIR in the image.
COPY Procfile app.json /app/

USER 10001:10001

# No EXPOSE: dokku then routes to its default port, 5000, and sets PORT to match. No ENTRYPOINT, so the
# Procfile command and app.json's predeploy command (/app/migrate up) run as given.
CMD ["/app/bardic-torch"]
