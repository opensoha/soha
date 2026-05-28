FROM node:20-bookworm-slim AS web-build
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN for attempt in 1 2 3; do \
      npm ci && exit 0; \
      echo "npm ci failed on attempt ${attempt}, retrying..." >&2; \
      sleep 2; \
    done; \
    exit 1
COPY web/ ./
RUN npm run build

FROM node:20-bookworm-slim AS docs-build
WORKDIR /src/docs
ENV DOCS_SHOW_LAST_UPDATE_TIME=false
COPY docs/package.json docs/package-lock.json ./
RUN for attempt in 1 2 3; do \
      npm ci && exit 0; \
      echo "npm ci failed on attempt ${attempt}, retrying..." >&2; \
      sleep 2; \
    done; \
    exit 1
COPY docs/ ./
COPY deploy/ ../deploy/
RUN npm run build

FROM golang:1.23-alpine AS go-build
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN for attempt in 1 2 3; do \
      go mod download && exit 0; \
      echo "go mod download failed on attempt ${attempt}, retrying..." >&2; \
      sleep 2; \
    done; \
    exit 1
COPY . .
COPY --from=web-build /src/web/dist ./web/dist
COPY --from=docs-build /src/docs/build ./docs/build
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -tags embedassets -trimpath -ldflags="-s -w" -o /out/soha ./cmd/server

FROM alpine:3.20
RUN addgroup -S soha && adduser -S -G soha soha && apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=go-build /out/soha /app/soha
COPY configs /app/configs
COPY migrations /app/migrations
ENV SOHA_CONFIG_FILE=/app/configs/config.yaml
USER soha
EXPOSE 8080
ENTRYPOINT ["/app/soha"]
