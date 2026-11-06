FROM --platform=$BUILDPLATFORM golang:1.27.3-alpine AS agent-build
ARG TARGETOS=linux
ARG TARGETARCH
ARG GOPROXY=https://proxy.golang.org,direct
ARG VERSION=dev
ARG COMMIT=unknown
ARG DATE=unknown
ENV GOPROXY=$GOPROXY
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./soha-agent/
COPY --from=contracts go.mod go.sum ./soha-contracts/
RUN for attempt in 1 2 3; do \
      printf 'go 1.27.3\n\nuse (\n\t./soha-agent\n\t./soha-contracts\n)\n' > go.work; \
      (cd soha-agent && go mod download) && exit 0; \
      echo "go mod download failed on attempt ${attempt}, retrying..." >&2; \
      sleep 2; \
    done; \
    exit 1
COPY --from=contracts . ./soha-contracts
COPY . ./soha-agent
ARG GOMAXPROCS=4
ENV GOMAXPROCS=$GOMAXPROCS
RUN if [ -z "$TARGETARCH" ]; then TARGETARCH="$(go env GOARCH)"; fi; \
    cd soha-agent && CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w -X github.com/opensoha/soha-agent/internal/agent/buildinfo.Version=${VERSION} -X github.com/opensoha/soha-agent/internal/agent/buildinfo.Commit=${COMMIT} -X github.com/opensoha/soha-agent/internal/agent/buildinfo.Date=${DATE}" -o /out/soha-agent ./cmd/agent

FROM docker:29.4.0-cli@sha256:2efe7c8e806b35050def6ba1ba0ddad67b521a0a6d37f4910b3d88f11b495d03 AS buildpacks-runtime
ARG TARGETARCH
RUN apk add --no-cache ca-certificates git openssh-client tzdata \
    && case "$TARGETARCH" in \
         arm64) PACK_ARCH=linux-arm64; PACK_SHA=091ccb213823656c727731537ef8f1000eb4dc3ec61641506653e7f9d6da0c5e ;; \
         amd64) PACK_ARCH=linux; PACK_SHA=dc0ee1e931cf8a106d7555a01a214864f9acb60b77adf15d69b74df4404758e9 ;; \
         *) echo "unsupported Buildpacks architecture" >&2; exit 1 ;; \
       esac \
    && wget -q "https://github.com/buildpacks/pack/releases/download/v0.40.9/pack-v0.40.9-${PACK_ARCH}.tgz" -O /tmp/pack.tgz \
    && printf '%s  /tmp/pack.tgz\n' "$PACK_SHA" | sha256sum -c - \
    && tar -xzf /tmp/pack.tgz -C /usr/local/bin pack \
    && rm /tmp/pack.tgz \
    && addgroup -S -g 10001 soha && adduser -S -D -H -u 10001 -G soha soha \
    && mkdir -p /var/lib/soha-agent /var/log/soha-agent \
    && chown -R 10001:10001 /var/lib/soha-agent /var/log/soha-agent
WORKDIR /app
COPY --from=agent-build /out/soha-agent /app/soha-agent
COPY configs/agent.config.yaml /app/configs/agent.config.yaml
ENV SOHA_AGENT_CONFIG_FILE=/app/configs/agent.config.yaml
USER 10001:10001
EXPOSE 18080
ENTRYPOINT ["/app/soha-agent"]

FROM buildpacks-runtime AS buildpacks-daemonless-runtime
USER 0:0
RUN apk add --no-cache podman=5.7.0-r6 fuse-overlayfs=1.16-r0

FROM alpine:3.20 AS agent-runtime
RUN addgroup -S -g 10001 soha && adduser -S -D -H -u 10001 -G soha soha && apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=agent-build /out/soha-agent /app/soha-agent
COPY configs/agent.config.yaml /app/configs/agent.config.yaml
RUN mkdir -p /app/configs /var/lib/soha-agent /var/log/soha-agent \
    && chown -R 10001:10001 /var/lib/soha-agent /var/log/soha-agent
ENV SOHA_AGENT_CONFIG_FILE=/app/configs/agent.config.yaml
USER 10001:10001
EXPOSE 18080
ENTRYPOINT ["/app/soha-agent"]
