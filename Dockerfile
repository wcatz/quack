FROM golang:1.24-bookworm AS builder

ARG VERSION=dev
ARG COMMIT_SHA=unknown
ARG BUILD_DATE=unknown

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo \
    -ldflags "-X main.version=${VERSION} -X main.commitSHA=${COMMIT_SHA} -X main.buildDate=${BUILD_DATE}" \
    -o quack ./cmd/quack/

FROM python:3.12-slim-bookworm

RUN pip install --no-cache-dir gallery-dl && \
    apt-get update && apt-get install -y --no-install-recommends ca-certificates && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY --from=builder /app/quack /app/quack
RUN mkdir -p /data /tmp/downloads

USER 1000:1000

EXPOSE 8080
CMD ["/app/quack"]
