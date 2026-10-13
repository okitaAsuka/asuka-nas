# Explicit daemon container entrypoint; no management port exposed.
FROM golang:1.26.9-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -o /out/asuka-nas ./cmd/asuka-nas

FROM debian:bookworm-slim
COPY --from=build /out/asuka-nas /usr/local/bin/asuka-nas
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/asuka-nas"]
CMD ["-run", "-config", "/config/config.json"]
