# Stage 1: Build frontend
FROM node:25-alpine AS frontend-builder

RUN npm install -g pnpm@11.3.0

WORKDIR /frontend

# Copy frontend package files
COPY frontend/package.json frontend/pnpm-lock.yaml frontend/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile

# Copy frontend source and build
COPY frontend/ ./
RUN pnpm run build

# Stage 2: Build backend
FROM golang:1.27.1-alpine AS backend-builder

WORKDIR /backend

# Install build dependencies
RUN apk add --no-cache git

# Copy go mod files and download dependencies
COPY backend/go.mod backend/go.sum ./
RUN go mod download

# Copy backend source and build
COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -o autoget ./cmd/main.go

# Stage 3: Deploy image
FROM alpine:latest AS deploy

# Install runtime dependencies
RUN apk --no-cache add ca-certificates tzdata

# Create necessary directories
RUN mkdir -p /html /app /config

# Add user 1000:1000 as specified
RUN addgroup -g 1000 -S appgroup && \
    adduser -u 1000 -S appuser -G appgroup

# Copy frontend build dist to /html
COPY --from=frontend-builder /frontend/dist /html

# Copy backend to /app
COPY --from=backend-builder /backend/autoget /app/

# Set ownership
RUN chown -R 1000:1000 /html /app /config

# Switch to non-root user
USER 1000:1000

# Set working directory
WORKDIR /app

# Run command: /app/autoget -c /config/config.yaml
CMD ["/app/autoget", "-c", "/config/config.yaml"]
