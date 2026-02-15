# ---- Stage: Official Node runtime (trixie-slim) ----
FROM node:trixie-slim AS node

# ---- Stage: Official Go toolchain (trixie) ----
FROM golang:trixie AS golang

# ---- Final: Python slim-trixie + Node + Go ----
FROM python:slim-trixie

# Set Python environment variables for optimization
ENV PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    PIP_NO_CACHE_DIR=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1

# Install essential packages and gotty dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates curl git htop iproute2 libatomic1 lsof \
    net-tools procps psmisc socat unzip wget zip \
 && rm -rf /var/lib/apt/lists/*

# Install gotty binary (multi-arch via TARGETARCH)
ARG TARGETARCH
RUN ARCH="${TARGETARCH:-amd64}" && \
    wget -q "https://github.com/sorenisanerd/gotty/releases/download/v1.6.0/gotty_v1.6.0_linux_${ARCH}.tar.gz" \
    && tar -xzf "gotty_v1.6.0_linux_${ARCH}.tar.gz" -C /usr/local/bin \
    && rm "gotty_v1.6.0_linux_${ARCH}.tar.gz" \
    && chmod +x /usr/local/bin/gotty \
    && echo "Gotty version:" && gotty --version

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

# Create working directory, auth config directory, and set ownership
RUN mkdir -p /app /home/opencode/.local/share/opencode && \
    chown -R opencode:opencode /app /home/opencode/.local

# Copy entrypoint script and gotty config (must be done as root)
COPY sandbox/entrypoint.sh /usr/local/bin/entrypoint.sh
COPY sandbox/.gotty /app/.gotty
RUN chmod +x /usr/local/bin/entrypoint.sh

# Switch to unprivileged user
USER opencode

# Set working directory
WORKDIR /app

# Expose OpenCode and gotty ports
EXPOSE 8080 8081

# Health check to verify OpenCode is running
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8080/health || exit 1

# Use the entrypoint script to start both services
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
