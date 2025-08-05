# Build stage
FROM golang:1.21-alpine AS builder

# Install build dependencies
RUN apk add --no-cache gcc musl-dev sqlite-dev

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY main.go ./

# Build the application
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o slideshow main.go

# Runtime stage
FROM alpine:latest

# Install runtime dependencies
RUN apk add --no-cache sqlite ca-certificates

# Create app directory
WORKDIR /app

# Create necessary directories and volume mount points
RUN mkdir -p /app/templates \
    && mkdir -p /app/DB \
    && mkdir -p /app/test2

# Copy the binary from builder stage
COPY --from=builder /app/slideshow .

# Copy templates
COPY templates/ ./templates/

# Create volumes for the specific paths your app needs
VOLUME ["/home/pi/go/slideshowgodocker/DB", "/home/pi/Pictures/test2"]

# Expose port
EXPOSE 8010

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/ || exit 1

# Run the application
CMD ["./slideshow"]