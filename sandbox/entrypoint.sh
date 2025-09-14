#!/bin/bash
# Docker entrypoint script for OpenCode sandbox
# Manages both OpenCode server and GoTTY terminal emulator

# Enable verbose logging for debugging
set -x

# Configuration
GOTTY_PORT=8081
OPENCODE_PORT=8080

# Start GoTTY in background
echo "Starting GoTTY on port ${GOTTY_PORT}..."
gotty \
    --permit-write \
    --reconnect \
    --reconnect-time 5 \
    --port ${GOTTY_PORT} \
    --address 0.0.0.0 \
    --ws-origin ".*" \
    --config /app/.gotty \
    /bin/bash 2>&1 | sed "s/^/[GOTTY] /" &
GOTTY_PID=$!
echo "GoTTY started with PID ${GOTTY_PID}"

# Start OpenCode in background
echo "Starting OpenCode on port ${OPENCODE_PORT}..."
opencode serve \
    --hostname 0.0.0.0 \
    --port ${OPENCODE_PORT} 2>&1 | sed "s/^/[OPENCODE] /" &
OPENCODE_PID=$!
echo "OpenCode started with PID ${OPENCODE_PID}"

# Function to handle graceful shutdown
shutdown() {
    echo "Shutting down..."
    # Send SIGTERM to both processes
    if [ -n "$GOTTY_PID" ]; then
        kill $GOTTY_PID 2>/dev/null
    fi
    if [ -n "$OPENCODE_PID" ]; then
        kill $OPENCODE_PID 2>/dev/null
    fi
    # Wait a moment for graceful shutdown
    sleep 1
    # Force kill if still running
    if [ -n "$GOTTY_PID" ]; then
        kill -9 $GOTTY_PID 2>/dev/null
    fi
    if [ -n "$OPENCODE_PID" ]; then
        kill -9 $OPENCODE_PID 2>/dev/null
    fi
    exit 0
}

# Set up signal handlers for graceful shutdown
trap shutdown SIGTERM SIGINT

# Wait for both background processes
# This will block until one of them exits
wait

# If we get here, one of the processes died unexpectedly
echo "Process exited unexpectedly, shutting down..."
shutdown