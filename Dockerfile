# syntax=docker/dockerfile:1.7

###############################################################################
# Frontend build stage (Node + Vite)
###############################################################################
FROM node:20-alpine AS frontend

WORKDIR /app/web

COPY web/package.json web/package-lock.json ./
RUN npm ci

COPY web/ .
RUN npm run build

###############################################################################
# Backend build stage (Go)
###############################################################################
FROM golang:1.25-alpine AS backend

WORKDIR /src

RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=frontend /app/internal/httpserver/assets /tmp/web-assets

RUN cp internal/httpserver/assets/api.html /tmp/api.html && \
    rm -rf internal/httpserver/assets/* && \
    mv /tmp/api.html internal/httpserver/assets/api.html && \
    cp -r /tmp/web-assets/. internal/httpserver/assets/

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown

RUN mkdir -p /out && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath \
    -ldflags "-s -w -X main.buildVersion=${VERSION} -X main.buildCommit=${COMMIT} -X main.buildTime=${BUILD_TIME}" \
    -o /out/amdgputop-web ./cmd/amdgputop-web

###############################################################################
# Runtime stage (Alpine, non-root)
###############################################################################
FROM alpine:3.23 AS runtime

RUN addgroup -S app && adduser -S -G app app && \
    apk add --no-cache ca-certificates wget

WORKDIR /home/app

COPY --from=backend /out/amdgputop-web /usr/local/bin/amdgputop-web

ENV APP_LISTEN_ADDR=:8080
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/healthz >/dev/null || exit 1

USER app
ENTRYPOINT ["/usr/local/bin/amdgputop-web"]
