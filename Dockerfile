# syntax=docker/dockerfile:1.7
FROM node:24.10-alpine AS web-build
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

FROM golang:1.27.0-alpine AS go-build
WORKDIR /src
RUN apk add --no-cache ca-certificates tzdata upx
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN rm -rf internal/webui/dist && mkdir -p internal/webui/dist
COPY --from=web-build /src/web/dist/ ./internal/webui/dist/
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/mail-aggregator ./cmd/server \
    && upx --best --lzma /out/mail-aggregator \
    && upx --test /out/mail-aggregator

# Bind-mounted data directories are owned by the host. Allow deployments to
# match the unprivileged container account to the host account that owns data/.
ARG APP_UID=1000
ARG APP_GID=1000
RUN addgroup -S -g "${APP_GID}" app \
    && adduser -S -u "${APP_UID}" -G app -h /app app \
    && mkdir -p /out/runtime/app/data /out/runtime/app/mail-proxy-certs \
    && chown -R app:app /out/runtime/app

FROM scratch
COPY --from=go-build /etc/passwd /etc/group /etc/
COPY --from=go-build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=go-build /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=go-build /out/runtime/app /app
WORKDIR /app
COPY --from=go-build /out/mail-aggregator /app/mail-aggregator
USER app
EXPOSE 8080
VOLUME ["/app/data"]
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 CMD ["/app/mail-aggregator", "--healthcheck"]
ENTRYPOINT ["/app/mail-aggregator"]
