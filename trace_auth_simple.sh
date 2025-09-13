#!/bin/bash

echo "=== AUTH CONFIG TRACE ==="
echo ""
echo "STEP 1: Checking auth.json in home directory"
echo "------------------------------------------------"

AUTH_FILE="$HOME/.local/share/opencode/auth.json"

if [ -f "$AUTH_FILE" ]; then
    echo "✓ Auth file exists at: $AUTH_FILE"
    echo "✓ File size: $(stat -c%s "$AUTH_FILE" 2>/dev/null || stat -f%z "$AUTH_FILE") bytes"
    echo ""
    echo "Providers in auth.json:"
    # Extract provider names from JSON keys
    jq -r 'keys[]' "$AUTH_FILE" 2>/dev/null | while read provider; do
        TYPE=$(jq -r ".\"$provider\".type" "$AUTH_FILE" 2>/dev/null)
        echo "  - $provider (type: $TYPE)"
        if [ "$TYPE" = "api" ]; then
            KEY_LEN=$(jq -r ".\"$provider\".key" "$AUTH_FILE" 2>/dev/null | wc -c)
            echo "    API key length: $KEY_LEN chars"
        elif [ "$TYPE" = "oauth" ]; then
            HAS_ACCESS=$(jq -r ".\"$provider\".access" "$AUTH_FILE" 2>/dev/null)
            HAS_REFRESH=$(jq -r ".\"$provider\".refresh" "$AUTH_FILE" 2>/dev/null)
            [ "$HAS_ACCESS" != "null" ] && echo "    Has access token: yes" || echo "    Has access token: no"
            [ "$HAS_REFRESH" != "null" ] && echo "    Has refresh token: yes" || echo "    Has refresh token: no"
        fi
    done
else
    echo "❌ Auth file not found at: $AUTH_FILE"
    exit 1
fi

echo ""
echo "STEP 2: Starting opencode-chat server to inspect providers"
echo "------------------------------------------------"

# Build the server
echo "Building opencode-chat..."
go build -o opencode-chat *.go 2>/dev/null
if [ $? -ne 0 ]; then
    echo "❌ Failed to build opencode-chat"
    exit 1
fi

# Start the server in background
echo "Starting server on port 8081..."
./opencode-chat -port 8081 > trace_server.log 2>&1 &
SERVER_PID=$!

# Function to cleanup on exit
cleanup() {
    echo ""
    echo "Cleaning up..."
    kill $SERVER_PID 2>/dev/null
    wait $SERVER_PID 2>/dev/null
    rm -f trace_server.log
}
trap cleanup EXIT

# Wait for server to start
echo "Waiting for server to start..."
for i in {1..30}; do
    curl -s http://localhost:8081/ > /dev/null 2>&1 && break
    sleep 1
done

if ! curl -s http://localhost:8081/ > /dev/null 2>&1; then
    echo "❌ Server failed to start. Check trace_server.log for details:"
    tail -20 trace_server.log
    exit 1
fi

echo "✓ Server started successfully"

# Wait a bit more for OpenCode to initialize
echo "Waiting for OpenCode to initialize..."
sleep 5

echo ""
echo "STEP 3: Querying available providers from OpenCode"
echo "------------------------------------------------"

# The server should have loaded providers by now
# Check the server log for provider information
echo "Checking server logs for provider information..."
grep -A 5 "Loaded providers" trace_server.log || echo "No provider info in logs yet"

echo ""
echo "Attempting to query OpenCode directly..."
# Get the OpenCode port (server port + 1000)
OPENCODE_PORT=9081

# Try to query providers directly
echo "Fetching from http://localhost:$OPENCODE_PORT/config/providers"
PROVIDERS_RESPONSE=$(curl -s http://localhost:$OPENCODE_PORT/config/providers 2>/dev/null)

if [ $? -eq 0 ] && [ -n "$PROVIDERS_RESPONSE" ]; then
    echo "✓ Got response from OpenCode"
    echo ""
    echo "Available providers:"
    echo "$PROVIDERS_RESPONSE" | jq -r '.providers[] | "  - \(.id): \(.name) (\(.models | length) models)"' 2>/dev/null || echo "$PROVIDERS_RESPONSE"

    echo ""
    echo "Default model:"
    echo "$PROVIDERS_RESPONSE" | jq -r '.default' 2>/dev/null
else
    echo "❌ Could not query OpenCode directly"
fi

echo ""
echo "STEP 4: Analysis from server logs"
echo "------------------------------------------------"
grep -E "(Loading auth config|Loaded auth config|providers|models)" trace_server.log | head -20

echo ""
echo "=== TRACE COMPLETE ==="
echo ""
echo "Full server log available in: trace_server.log"