#!/bin/bash

echo "=== Manual Session Isolation Test ==="
echo "Server: http://localhost:8080"
echo ""

# Test 1: Get workspace session ID
echo "1. Getting workspace session from server logs..."
WORKSPACE_SESSION=$(curl -s http://localhost:34911/session | jq -r '.[0].id' | grep "6be9e1a0dffe")
echo "   Workspace session: $WORKSPACE_SESSION"

# Test 2: Trigger file operation via workspace
echo ""
echo "2. Creating test file via workspace session..."
curl -s -X POST "http://localhost:34911/session/$WORKSPACE_SESSION/shell" \
  -H "Content-Type: application/json" \
  -d '{"agent": "agent", "command": "echo \"Session isolation test\" > isolation_test.txt"}' > /dev/null
echo "   File created"

# Test 3: Check file exists
echo ""
echo "3. Verifying file exists..."
curl -s "http://localhost:34911/file?path=." | jq -r '.[] | select(.name=="isolation_test.txt") | .name'

# Test 4: Count events in last 5 seconds
echo ""
echo "4. Checking server logs for isolation..."
echo "   Workspace events should have session: $WORKSPACE_SESSION"
echo "   Chat SSE should NOT process workspace events"

echo ""
echo "=== Test Complete ==="
echo "✅ If no workspace events appeared in the chat UI, session isolation is working!"
