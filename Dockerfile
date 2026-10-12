# Build-only scaffold. No daemon or real service integration is enabled yet.
FROM golang:1.26.9-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/asuka-nas ./cmd/asuka-nas

FROM debian:bookworm-slim
COPY --from=build /out/asuka-nas /usr/local/bin/asuka-nas
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/asuka-nas"]
CMD ["-config", "/config/config.json"]
