# Build stage
FROM golang:1.24-bookworm AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /watchpost ./cmd/watchpost

# Runtime stage
FROM debian:bookworm-slim

LABEL org.opencontainers.image.source=https://github.com/rvben/watchpost
LABEL org.opencontainers.image.description="Watchpost NVR - lightweight network video recorder"
LABEL org.opencontainers.image.licenses=MIT

RUN apt-get update && \
    apt-get install -y --no-install-recommends ffmpeg ca-certificates wget && \
    rm -rf /var/lib/apt/lists/*

RUN groupadd -r watchpost && useradd -r -g watchpost -d /data -s /sbin/nologin watchpost

RUN mkdir -p /data/recordings /data/snapshots /config && \
    chown -R watchpost:watchpost /data

COPY --from=builder /watchpost /usr/local/bin/watchpost

USER watchpost

EXPOSE 5050

VOLUME ["/data", "/config"]

ENTRYPOINT ["watchpost"]
CMD ["-config", "/config/config.yml"]
