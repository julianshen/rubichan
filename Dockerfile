# syntax=docker/dockerfile:1

FROM golang:1.26 AS builder
WORKDIR /src

RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates git build-essential \
	&& rm -rf /var/lib/apt/lists/*

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=1 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} go build -trimpath -ldflags='-s -w' -o /out/rubichan ./cmd/rubichan

FROM gcr.io/distroless/cc-debian12:nonroot
WORKDIR /app
COPY --from=builder /out/rubichan /usr/local/bin/rubichan
ENTRYPOINT ["/usr/local/bin/rubichan"]
CMD ["--help"]
