# syntax=docker/dockerfile:1
FROM golang:1.26.9@sha256:d7722066f0b60ceccb6c0643cbed1f5f9e15506ac237146c95333504d7805d89 AS build

ENV CGO_ENABLED=0 \
    GOTOOLCHAIN=local \
    GOFLAGS=-trimpath
WORKDIR /src/backend

COPY backend/go.mod backend/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY backend/ ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    mkdir -p /out && \
    for service in ingest detector event-writer alert-writer query-api; do \
      go build -ldflags="-s -w" -o "/out/${service}" "./cmd/${service}" || exit 1; \
    done && \
    go build -ldflags="-s -w" -o /out/healthprobe ./tools/healthprobe && \
    test ! -e /src/backend/.env

FROM gcr.io/distroless/static:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3

WORKDIR /app
COPY --from=build --chown=65532:65532 /out/ /app/
USER 65532:65532
ENTRYPOINT ["/app/ingest"]
