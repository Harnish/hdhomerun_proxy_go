# Build stage
FROM golang:1.25.5-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum* ./

# Download dependencies
RUN go mod download

# Copy source code
COPY *.go ./

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o hdhomerun_proxy .

# Runtime stage
FROM alpine:latest

WORKDIR /app

# Install ca-certificates for HTTPS support
RUN apk --no-cache add ca-certificates

# Copy binary from builder
COPY --from=builder /app/hdhomerun_proxy .

# Create a volume for configuration
VOLUME ["/app/config"]

# Default to app proxy mode listening on all interfaces
ENTRYPOINT ["./hdhomerun_proxy"]
CMD ["app", "0.0.0.0"]
