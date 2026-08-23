### ---- Builder: compile a static binary, nothing else ships from this stage ----
FROM golang:1.25-alpine AS builder

ARG VERSION=docker

WORKDIR /app

RUN apk add --no-cache ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/control-plane ./cmd/server


### ---- Runtime: distroless-style slim image, non-root user ----
FROM alpine:3.20 AS runtime

RUN apk add --no-cache ca-certificates curl \
    && addgroup -S app && adduser -S app -G app

WORKDIR /app

COPY --from=builder --chown=app:app /out/control-plane ./control-plane
COPY --from=builder --chown=app:app /app/static ./static

USER app

EXPOSE 8000

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD curl --fail --silent http://127.0.0.1:8000/ || exit 1

ENTRYPOINT ["./control-plane"]
