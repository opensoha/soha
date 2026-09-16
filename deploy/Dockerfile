FROM --platform=$BUILDPLATFORM golang:1.26.6-alpine AS go-source
ARG TARGETOS=linux
ARG TARGETARCH
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=$GOPROXY
WORKDIR /src
RUN for attempt in 1 2 3; do \
      timeout 120 apk add --no-cache git ca-certificates && exit 0; \
      echo "apk add failed on attempt ${attempt}, retrying..." >&2; \
      sleep 2; \
    done; \
    exit 1
COPY go.mod go.sum ./soha/
COPY --from=contracts go.mod go.sum ./soha-contracts/
RUN for attempt in 1 2 3; do \
      printf 'go 1.26.6\n\nuse (\n\t./soha\n\t./soha-contracts\n)\n' > go.work; \
      (cd soha && timeout 180 go mod download) && exit 0; \
      echo "go mod download failed on attempt ${attempt}, retrying..." >&2; \
      sleep 2; \
    done; \
    exit 1
COPY --from=contracts . ./soha-contracts
COPY . ./soha

FROM go-source AS go-build
ARG GOMAXPROCS=4
ENV GOMAXPROCS=$GOMAXPROCS
ARG SOHA_BUILD_VERSION=0.1.0
ARG TARGETOS=linux
ARG TARGETARCH
RUN test -f soha/internal/staticassets/web/dist/index.html
RUN if [ -z "$TARGETARCH" ]; then TARGETARCH="$(go env GOARCH)"; fi; \
    cd soha && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -tags embedassets -trimpath -ldflags="-s -w -X github.com/opensoha/soha/internal/application/plugin.currentSohaVersion=${SOHA_BUILD_VERSION}" -o /out/soha ./cmd/server && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/network-control ./cmd/network-control && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/ingest ./cmd/ingest && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/radius-adapter ./cmd/radius-adapter

FROM go-source AS network-gateway-build
ARG TARGETOS=linux
ARG TARGETARCH
RUN if [ -z "$TARGETARCH" ]; then TARGETARCH="$(go env GOARCH)"; fi; \
    cd soha && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/network-gateway ./cmd/network-gateway

FROM --platform=linux/amd64 freeradius/freeradius-server:3.2.10@sha256:cc7fd136e7b03e7b332d94297530318e824a4ecfedbce54562cced723e71e812 AS radius-runtime
RUN rm -f /etc/freeradius/sites-enabled/default /etc/freeradius/sites-enabled/inner-tunnel /etc/freeradius/mods-enabled/eap
COPY deploy/freeradius/default /etc/freeradius/sites-enabled/default
COPY deploy/freeradius/eap /etc/freeradius/mods-enabled/eap
COPY deploy/freeradius/soha /etc/freeradius/mods-enabled/soha
COPY deploy/freeradius/clients.conf /etc/freeradius/clients.conf
COPY deploy/freeradius/entrypoint.sh /soha-radius-entrypoint.sh
COPY --from=go-build /out/radius-adapter /usr/local/bin/radius-adapter
RUN chmod 0755 /soha-radius-entrypoint.sh /usr/local/bin/radius-adapter
EXPOSE 1812/udp 1813/udp
ENTRYPOINT ["/soha-radius-entrypoint.sh"]
CMD ["freeradius"]

FROM alpine:3.20 AS network-gateway-runtime
RUN for attempt in 1 2 3; do \
      timeout 120 apk add --no-cache ca-certificates tzdata iproute2 nftables && exit 0; \
      echo "apk add failed on attempt ${attempt}, retrying..." >&2; \
      sleep 2; \
    done; \
    exit 1
WORKDIR /app
COPY --from=network-gateway-build /out/network-gateway /app/network-gateway
RUN mkdir -p /var/lib/soha-network-gateway && chmod 0700 /var/lib/soha-network-gateway
USER 0:0
EXPOSE 51820/udp 8084
ENTRYPOINT ["/app/network-gateway"]
CMD ["run"]

FROM alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc AS network-test-runtime
RUN for attempt in 1 2 3; do \
      timeout 120 apk add --no-cache ca-certificates curl iproute2 wireguard-tools && exit 0; \
      echo "apk add failed on attempt ${attempt}, retrying..." >&2; \
      sleep 2; \
    done; \
    exit 1
ENTRYPOINT ["tail", "-f", "/dev/null"]

FROM debian:bookworm-slim@sha256:88200866dfff7ea7f5cbcb6ec7c8a701889efe6fe859fe64d6990e4b07ea4171 AS radius-eap-test-runtime
RUN for attempt in 1 2 3; do \
      timeout 120 apt-get update && \
      timeout 120 apt-get install -y --no-install-recommends ca-certificates eapoltest && \
      rm -rf /var/lib/apt/lists/* && exit 0; \
      echo "apt install failed on attempt ${attempt}, retrying..." >&2; \
      sleep 2; \
    done; \
    exit 1
COPY deploy/network-test/eapol-test.sh /usr/local/bin/soha-eapol-test
RUN chmod 0755 /usr/local/bin/soha-eapol-test
ENTRYPOINT ["/usr/local/bin/soha-eapol-test"]

FROM alpine:3.20 AS soha-runtime
RUN addgroup -S -g 10001 soha && adduser -S -D -H -u 10001 -G soha soha && apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=go-build /out/soha /app/soha
COPY --from=go-build /out/network-control /app/network-control
COPY --from=go-build /out/ingest /app/ingest
COPY configs /app/configs
COPY migrations /app/migrations
COPY deploy/entrypoint.sh /app/entrypoint.sh
ENV SOHA_CONFIG_FILE=/app/configs/config.yaml \
    SOHA_PLUGINS_COMPANION_STORAGE_DIR=/app/data/companion
RUN mkdir -p /app/data && chown 10001:10001 /app/data && chmod 0755 /app/entrypoint.sh
USER 10001:10001
EXPOSE 8080 8082 8083
ENTRYPOINT ["/app/entrypoint.sh"]
CMD ["/app/soha"]
