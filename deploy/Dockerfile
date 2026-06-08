FROM golang:1.23-alpine AS go-build
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./soha/
COPY --from=contracts go.mod ./soha-contracts/go.mod
RUN for attempt in 1 2 3; do \
      printf 'go 1.23.0\n\nuse (\n\t./soha\n\t./soha-contracts\n)\n\nreplace github.com/opensoha/soha-contracts v0.0.0 => ./soha-contracts\n' > go.work; \
      cd soha && go mod download && exit 0; \
      echo "go mod download failed on attempt ${attempt}, retrying..." >&2; \
      sleep 2; \
    done; \
    exit 1
COPY --from=contracts . ./soha-contracts
COPY . ./soha
RUN test -f soha/internal/staticassets/web/dist/index.html
RUN cd soha && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -tags embedassets -trimpath -ldflags="-s -w" -o /out/soha ./cmd/server

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
