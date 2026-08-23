### ---- CSS builder: compile the Tailwind source into static/app.css ----
FROM node:22-alpine AS css-builder

WORKDIR /app

COPY package.json package-lock.json ./
RUN npm ci

COPY static/src ./static/src
RUN npm run build:css


### ---- Builder: regenerate templ views, compile a static binary ----
FROM golang:1.25-alpine AS builder

ARG VERSION=docker

WORKDIR /app

RUN apk add --no-cache ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=css-builder /app/static/app.css ./static/app.css

RUN go run github.com/a-h/templ/cmd/templ generate
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
