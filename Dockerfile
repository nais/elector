# Build the manager binary
FROM --platform=$BUILDPLATFORM golang:1.26 AS builder

WORKDIR /workspace

# Copy the Go Modules manifests
COPY go.* .

# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

ARG TARGETOS TARGETARCH
ENV GOOS=$TARGETOS GOARCH=$TARGETARCH
RUN go build std

# Copy rest of project
COPY . /workspace

# Run tests
RUN make test

# Build
RUN CGO_ENABLED=0 go build -o elector cmd/elector/main.go

FROM scratch
WORKDIR /

# ADD https://curl.haxx.se/ca/cacert.pem /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /workspace/elector .

ENTRYPOINT ["/elector"]
