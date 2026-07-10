# ============================================================
# RDC Server — Multi-stage Dockerfile (T-6.9)
# ============================================================
# Build stage: compile the Go binary
# Runtime stage: minimal image with only the binary + migrations
# ============================================================

# --- Build stage ---
FROM golang:1.25-alpine AS builder

# Install git (needed for go modules with private deps, if any)
RUN apk add --no-cache git

# Set working directory
WORKDIR /build

# Copy go.mod and go.sum first (better layer caching)
COPY source/go.mod source/go.sum ./

# Download dependencies
RUN go mod download

# Copy the source code
COPY source/ .

# Build the binary
# CGO_ENABLED=0: static binary, no C dependencies
# GOOS=linux: target Linux
# -ldflags="-s -w": strip debug info for smaller image
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /rdc-server .

# --- Runtime stage ---
FROM alpine:3.20

# Install ca-certificates (needed for HTTPS calls to LW, SIMA, MyGov, SMS gateway)
# and tzdata (for correct timezone handling in logs)
RUN apk add --no-cache ca-certificates tzdata

# Create a non-root user for security
RUN adduser -D -h /app appuser

# Set working directory
WORKDIR /app

# Copy the binary from the build stage
COPY --from=builder /rdc-server /app/rdc-server

# Copy migrations (needed at startup)
COPY source/migrations /app/migrations

# Switch to non-root user
USER appuser

# Expose the HTTP port
EXPOSE 8000

# Health check — hit the server's listen endpoint
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -q --spider http://localhost:8000/api/applications/1 || exit 1

# Run the server
ENTRYPOINT ["/app/rdc-server"]
