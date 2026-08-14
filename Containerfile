# syntax=docker/dockerfile:1.7

FROM node:26-alpine AS web-builder
WORKDIR /src

RUN npm install --global pnpm@11.13.1
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml tsconfig.json ./
COPY .config .config
COPY apps/dash/package.json apps/dash/package.json
RUN pnpm install --frozen-lockfile

COPY apps/dash apps/dash
COPY assets/icons assets/icons
RUN pnpm --filter @omnisave/dash run build

FROM golang:1.26-alpine AS server-builder
ARG TARGETOS=linux
ARG TARGETARCH
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download
COPY cmd cmd
COPY internal internal
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/omnisave-server ./cmd/server

FROM alpine:3.23
LABEL org.opencontainers.image.title="Omnisave" \
      org.opencontainers.image.description="Self-hosted, versioned game-save synchronization" \
      org.opencontainers.image.source="https://github.com/krispya/omnisave" \
      org.opencontainers.image.licenses="ISC"

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S -g 10001 omnisave \
    && adduser -S -D -H -u 10001 -G omnisave omnisave \
    && mkdir -p /app/web /data \
    && chown -R omnisave:omnisave /data

WORKDIR /app
COPY --from=server-builder /out/omnisave-server /app/omnisave-server
COPY --from=web-builder /src/apps/dash/dist /app/web

ENV OMNISAVE_LISTEN_ADDR=:8080 \
    OMNISAVE_STORE_DIR=/data/store \
    OMNISAVE_DB_PATH=/data/omnisave.db \
    OMNISAVE_WEB_DIR=/app/web

VOLUME ["/data"]
EXPOSE 8080
USER omnisave

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -q -O - http://127.0.0.1:8080/healthz >/dev/null || exit 1

ENTRYPOINT ["/app/omnisave-server"]
