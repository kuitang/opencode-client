#!/bin/bash

# Send a message
echo "Sending test message..."
curl -s -X POST http://localhost:8080/send \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "message=What is 2 + 2?&provider=anthropic&model=claude-3-5-haiku-latest" \
  > /dev/null

echo "Message sent. Listening to SSE events..."
echo ""

# Listen to SSE events with timeout
timeout 10 curl -N -H "Accept: text/event-stream" http://localhost:8080/events 2>/dev/null | while IFS= read -r line; do
    if [[ $line == data:* ]]; then
        echo "SSE Event: $line"
        # Try to extract and decode the HTML
        html=$(echo "$line" | sed 's/^data: //')
        if [[ -n "$html" ]]; then
            echo "HTML content: $html"
            echo "---"
        fi
    fi
done

echo ""
echo "Test complete."