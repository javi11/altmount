# Multi-stage build for AltMount - Multi-platform optimized
# This is the self-contained development/local build Dockerfile
# For CI builds, use Dockerfile.ci which expects pre-built frontend assets
# Use buildkit syntax for advanced features
# syntax=docker/dockerfile:1.11

# Frontend build stage
FROM oven/bun:1 AS frontend-builder

# Set working directory for frontend
WORKDIR /app/frontend

# Copy only dependency manifests first to leverage caching for bun install
COPY frontend/bun.lock frontend/package.json ./

# Install dependencies with enhanced cache mounting
RUN --mount=type=cache,target=/root/.bun/install/cache,sharing=locked \
    --mount=type=cache,target=/tmp/bun-cache,sharing=locked \
    bun install --frozen-lockfile

# Copy the rest of the frontend sources and build with cache
COPY frontend/ ./
RUN --mount=type=cache,target=/tmp/bun-build-cache,sharing=locked \
    bun run build

# Backend build stage with native arch support (no cross-compilation)
FROM golang:1.24-alpine AS backend-builder

# Install build dependencies including sqlite with cache mount
RUN --mount=type=cache,target=/var/cache/apk,sharing=locked \
    apk add --no-cache git gcc g++ musl-dev sqlite-dev

# Set working directory
WORKDIR /app

# Copy go module files first for better caching
COPY go.mod go.sum ./

# Download dependencies with enhanced cache mount
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,target=/root/.cache/go-mod-download,sharing=locked \
    go mod download

# Copy source code in optimal order for caching
COPY ./pkg ./pkg
COPY ./internal ./internal
COPY ./cmd ./cmd
COPY ./frontend/embed_cli.go ./frontend/embed_cli.go
COPY ./frontend/embed_docker.go ./frontend/embed_docker.go

# Copy built frontend from previous stage
COPY --from=frontend-builder /app/frontend/dist ./frontend/dist

# Build web binary with enhanced cache mount for native architecture
RUN --mount=type=cache,target=/root/.cache/go-build,sharing=locked \
    --mount=type=cache,target=/tmp/go-build-cache,sharing=locked \
    CGO_ENABLED=1 GOOS=linux \
    go build -a -ldflags '-linkmode external -extldflags "-static"' -o altmount cmd/altmount/main.go

# Final stage - use architecture-specific base image with conditional selection
ARG TARGETARCH
FROM ghcr.io/linuxserver/baseimage-ubuntu:jammy

ARG DEBIAN_FRONTEND="noninteractive"
ARG BUILD_DATE
ARG VERSION
ARG PUID=1000
ARG PGID=1000

# Set up environment variables for PUID and PGID
ENV PUID=${PUID}
ENV PGID=${PGID}

# Set the working directory inside the container
WORKDIR /app

# Copy web binary from builder
COPY --from=backend-builder /app/altmount /app/altmount

# Install required packages for runtime
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        wget \
        ca-certificates && \
    apt-get install --reinstall mime-support -y && \
    rm -rf /var/lib/apt/lists/*

# Make binary executable
RUN chmod +x /app/altmount

# Copy s6-overlay service configuration
COPY docker/root/ /

# Set environment variables
ENV PORT=8080
ENV HOST=0.0.0.0

# Expose port for web interface
EXPOSE 8080

# Volume
VOLUME ["/config", "/metadata"]

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=30s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# Labels
LABEL org.opencontainers.image.source="https://github.com/javi11/altmount"
LABEL build_version="version: Build-date:- ${VERSION}  ${BUILD_DATE}"
LABEL maintainer="javi11"
