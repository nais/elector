# Build the manager binary
FROM --platform=$BUILDPLATFORM golang:1.22 as builder

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

# Download envtest tools early to avoid re-downloading on every code change
RUN make .envtest

# Run tests
RUN make test

# Run integration tests
RUN make integration_test

# Build
RUN CGO_ENABLED=0 go build -a -installsuffix cgo -o elector cmd/elector/main.go

FROM gcr.io/distroless/static-debian11
WORKDIR /
COPY --from=builder /workspace/elector /elector

ENTRYPOINT ["/elector"]
