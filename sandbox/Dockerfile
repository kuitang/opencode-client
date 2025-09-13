# ---- Stage: Official Node runtime (latest) ----
FROM node:slim AS node

# ---- Stage: Official Go toolchain (latest) ----  
FROM golang:latest AS golang

# ---- Final: Python slim + Node + Go ----
FROM python:slim

# Set Python environment variables for optimization
ENV PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    PIP_NO_CACHE_DIR=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1

# Install essential packages including tini for process supervision
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates curl unzip git tini zip \
 && rm -rf /var/lib/apt/lists/*

# Copy entire Node.js installation from the official Node image
COPY --from=node /usr/local/ /usr/local/

# Copy Go toolchain from the official Go image
COPY --from=golang /usr/local/go /usr/local/go
ENV PATH="/usr/local/go/bin:${PATH}"

# Install OpenCode globally with latest version
RUN npm install -g opencode-ai@latest

# Create unprivileged user following Python best practices
RUN groupadd -r -g 1001 opencode && \
    useradd -r -u 1001 -g opencode -m -d /home/opencode opencode

# Create working directory and set ownership
RUN mkdir -p /app && \
    chown -R opencode:opencode /app

# Create directories for auth configuration
RUN mkdir -p /home/opencode/.local/share/opencode && \
    chown -R opencode:opencode /home/opencode/.local

# Switch to unprivileged user
USER opencode

# Set working directory
WORKDIR /app

# Expose default OpenCode port
EXPOSE 8080

# Health check to verify OpenCode is running
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8080/health || exit 1

# Use tini for proper signal handling and process supervision
ENTRYPOINT ["/usr/bin/tini", "--", "opencode", "serve", "--hostname", "0.0.0.0", "--port", "8080"]