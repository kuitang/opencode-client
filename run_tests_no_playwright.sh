#!/bin/bash

# Simple test runner that doesn't require Playwright
# Tests core functionality using curl and basic checks

echo "================================"
echo "OpenCode Client Test Suite"
echo "No Playwright Required Version"
echo "================================"
echo ""

# Check if server is running
echo "1. Checking server status..."
if curl -s http://localhost:8085 > /dev/null 2>&1; then
    echo "   ✓ Server is running on port 8085"
else
    echo "   ✗ Server is not running"
    echo "   Please start with: ./opencode-chat -port 8085"
    exit 1
fi
echo ""

# Test 1: First load shows preview content
echo "2. Testing first load shows preview content..."
RESULT=$(curl -s http://localhost:8085/ | grep -c "No Application Running")
if [ "$RESULT" -gt 0 ]; then
    echo "   ✓ First load shows 'No Application Running' ($RESULT occurrences)"
else
    # Check for old placeholder
    PLACEHOLDER=$(curl -s http://localhost:8085/ | grep -c "Welcome to VibeCoding")
    if [ "$PLACEHOLDER" -gt 0 ]; then
        echo "   ✗ First load still shows placeholder text"
    else
        echo "   ⚠ Unexpected content on first load"
    fi
fi
echo ""

# Test 2: Preview tab endpoint works
echo "3. Testing Preview tab endpoint..."
PREVIEW=$(curl -s http://localhost:8085/tab/preview)
if echo "$PREVIEW" | grep -q "No Application Running\|preview-iframe"; then
    echo "   ✓ Preview tab endpoint returns expected content"

    # Check for Kill button when no server running
    if echo "$PREVIEW" | grep -q 'hx-post="/kill-preview-port"'; then
        echo "   ⚠ Kill button present when no server running (unexpected)"
    else
        echo "   ✓ Kill button correctly hidden when no server running"
    fi
else
    echo "   ✗ Preview tab endpoint returns unexpected content"
fi
echo ""

# Test 3: Kill endpoint exists and responds
echo "4. Testing Kill endpoint..."
KILL_RESPONSE=$(curl -s -X POST http://localhost:8085/kill-preview-port -d "port=9999" -w "\n%{http_code}")
HTTP_CODE=$(echo "$KILL_RESPONSE" | tail -n 1)
if [ "$HTTP_CODE" = "200" ]; then
    echo "   ✓ Kill endpoint responds with 200 OK"
    if echo "$KILL_RESPONSE" | grep -q "No Application Running"; then
        echo "   ✓ Kill endpoint returns preview content"
    fi
else
    echo "   ⚠ Kill endpoint returned HTTP $HTTP_CODE"
fi
echo ""

# Test 4: Test with a real server
echo "5. Testing with real server on port 5555..."
# Kill any existing server on 5555
lsof -ti:5555 | xargs -r kill 2>/dev/null

# Start Python server
python3 -m http.server 5555 > /dev/null 2>&1 &
SERVER_PID=$!
echo "   Started test server with PID $SERVER_PID"
sleep 3

# Check if preview detects the port
PREVIEW_WITH_SERVER=$(curl -s http://localhost:8085/tab/preview)
if echo "$PREVIEW_WITH_SERVER" | grep -q "Port 5555\|preview-iframe"; then
    echo "   ✓ Preview detects running server on port 5555"

    # Check for Kill button
    if echo "$PREVIEW_WITH_SERVER" | grep -q 'hx-post="/kill-preview-port"'; then
        echo "   ✓ Kill button appears when server is running"

        # Check for port value in form
        if echo "$PREVIEW_WITH_SERVER" | grep -q 'value="5555"'; then
            echo "   ✓ Kill button form has correct port value"
        else
            echo "   ✗ Kill button form missing port value"
        fi
    else
        echo "   ✗ Kill button not found when server is running"
    fi
else
    echo "   ✗ Preview does not detect server on port 5555"
fi

# Clean up test server
kill $SERVER_PID 2>/dev/null
echo "   Cleaned up test server"
echo ""

# Test 5: Static files
echo "6. Testing static file serving..."
if curl -s http://localhost:8085/static/styles.css | grep -q "css\|style"; then
    echo "   ✓ Static CSS file served correctly"
else
    echo "   ✗ Static CSS file not accessible"
fi

if curl -s http://localhost:8085/static/script.js | grep -q "function\|htmx"; then
    echo "   ✓ Static JS file served correctly"
else
    echo "   ✗ Static JS file not accessible"
fi
echo ""

# Test 6: Other tabs
echo "7. Testing other tab endpoints..."
for tab in code terminal deployment; do
    if curl -s http://localhost:8085/tab/$tab | grep -q "html\|div"; then
        echo "   ✓ /$tab endpoint works"
    else
        echo "   ✗ /$tab endpoint failed"
    fi
done
echo ""

# Summary
echo "================================"
echo "Test Summary"
echo "================================"
echo ""
echo "Core Features:"
echo "  • First load rendering: TESTED"
echo "  • Preview tab: TESTED"
echo "  • Kill button: TESTED"
echo "  • Port detection: TESTED"
echo ""
echo "To run full Playwright tests:"
echo "  1. npm install playwright"
echo "  2. node test_preview_integration.js"
echo ""
echo "To test manually in browser:"
echo "  1. Open http://localhost:8085"
echo "  2. Open DevTools Console (F12)"
echo "  3. Copy/paste test files and run functions"
echo ""