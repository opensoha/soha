FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS go-build
ARG TARGETOS=linux
ARG TARGETARCH
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=$GOPROXY
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./soha/
COPY --from=contracts go.mod go.sum ./soha-contracts/
RUN for attempt in 1 2 3; do \
      printf 'go 1.25.0\n\nuse (\n\t./soha\n\t./soha-contracts\n)\n\nreplace github.com/opensoha/soha-contracts v0.1.1 => ./soha-contracts\n' > go.work; \
      (cd soha && go mod download) && exit 0; \
      echo "go mod download failed on attempt ${attempt}, retrying..." >&2; \
      sleep 2; \
    done; \
    exit 1
COPY --from=contracts . ./soha-contracts
COPY . ./soha
RUN test -f soha/internal/staticassets/web/dist/index.html
RUN if [ -z "$TARGETARCH" ]; then TARGETARCH="$(go env GOARCH)"; fi; \
    cd soha && CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -tags embedassets -trimpath -ldflags="-s -w" -o /out/soha ./cmd/server

FROM alpine:3.20
RUN addgroup -S -g 10001 soha && adduser -S -D -H -u 10001 -G soha soha && apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=go-build /out/soha /app/soha
COPY configs /app/configs
COPY migrations /app/migrations
ENV SOHA_CONFIG_FILE=/app/configs/config.yaml
USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["/app/soha"]
