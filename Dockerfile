# syntax=docker/dockerfile:1

FROM node:24-alpine AS web-builder
WORKDIR /src
COPY package.json package-lock.json ./
COPY web/package.json web/package.json
RUN npm ci
COPY web web
RUN npm run web:build

FROM golang:1.26-alpine AS server-builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o /omnisave-server ./cmd/server

FROM alpine:3.23
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=server-builder /omnisave-server .
COPY --from=web-builder /src/web/dist /app/web

VOLUME ["/config", "/data"]
EXPOSE 8080

ENTRYPOINT ["/app/omnisave-server", "/config/server.yaml"]
