# --- Stage 1: Build ---
FROM golang:1.24.2-alpine3.21 AS builder

# Set up environment
WORKDIR /app

# Copy only go.mod and go.sum first (to leverage Docker layer caching)
COPY go.mod go.sum ./
RUN go mod download

# Now copy the rest of the source code
COPY . .

# Build the binary
RUN go build -o rest .

# --- Stage 2: Serve ---
FROM golang:1.24.2-alpine3.21 AS serve

WORKDIR /app

# Copy binary from builder stage
COPY --from=builder /app/rest .

# Expose the port your app listens on (optional, for docs)
EXPOSE 3000

# Run the app
CMD ["./rest"]
