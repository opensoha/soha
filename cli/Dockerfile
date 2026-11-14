FROM --platform=$BUILDPLATFORM golang:1.27.4-alpine AS cli-build
ARG TARGETOS=linux
ARG TARGETARCH
ARG GOPROXY=https://proxy.golang.org,direct
ARG VERSION=dev
ARG COMMIT=unknown
ARG DATE=unknown
ENV GOPROXY=$GOPROXY GOWORK=/src/go.work GOFLAGS=-mod=readonly
WORKDIR /src
RUN apk add --no-cache ca-certificates git
COPY go.mod go.sum go.work go.work.sum ./
COPY contracts/go.mod contracts/go.sum ./contracts/
COPY cli/go.mod cli/go.sum ./cli/
COPY agent/go.mod agent/go.sum ./agent/
WORKDIR /src/cli
COPY contracts/ /src/contracts/
COPY cli/ /src/cli/
RUN if [ -z "$TARGETARCH" ]; then TARGETARCH="$(go env GOARCH)"; fi; \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w -X github.com/opensoha/soha/cli/internal/sohacli.version=${VERSION} -X github.com/opensoha/soha/cli/internal/sohacli.commit=${COMMIT} -X github.com/opensoha/soha/cli/internal/sohacli.date=${DATE}" -o /out/soha ./cmd/soha

FROM alpine:3.20
ARG COMMIT=unknown
LABEL org.opencontainers.image.revision=$COMMIT
RUN apk add --no-cache ca-certificates tzdata
COPY --from=cli-build /out/soha /usr/local/bin/soha
ENTRYPOINT ["soha"]
