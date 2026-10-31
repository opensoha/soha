FROM --platform=$BUILDPLATFORM golang:1.27.3-alpine AS cli-build
ARG TARGETOS=linux
ARG TARGETARCH
ARG GOPROXY=https://proxy.golang.org,direct
ARG VERSION=dev
ARG COMMIT=unknown
ARG DATE=unknown
ENV GOPROXY=$GOPROXY
WORKDIR /src
RUN apk add --no-cache ca-certificates git
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN if [ -z "$TARGETARCH" ]; then TARGETARCH="$(go env GOARCH)"; fi; \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w -X github.com/opensoha/soha-cli/internal/sohacli.version=${VERSION} -X github.com/opensoha/soha-cli/internal/sohacli.commit=${COMMIT} -X github.com/opensoha/soha-cli/internal/sohacli.date=${DATE}" -o /out/soha ./cmd/soha

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
COPY --from=cli-build /out/soha /usr/local/bin/soha
ENTRYPOINT ["soha"]
