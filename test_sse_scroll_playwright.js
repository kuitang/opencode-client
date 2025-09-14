// Playwright test for SSE scroll behavior
// Tests that messages auto-scroll when user is at bottom, but preserve position when scrolled up

async function testSSEScrollBehavior() {
    const results = [];

    // Test 1: Initial page load scrolls to bottom
    const messagesDiv = document.getElementById('messages');
    if (!messagesDiv) {
        return { error: 'Messages div not found' };
    }

    // Check initial scroll position (should be at bottom after page load)
    const initialAtBottom = Math.abs(messagesDiv.scrollHeight - messagesDiv.scrollTop - messagesDiv.clientHeight) < 10;
    results.push({
        test: 'Initial load scrolls to bottom',
        passed: initialAtBottom,
        scrollTop: messagesDiv.scrollTop,
        scrollHeight: messagesDiv.scrollHeight
    });

    // Test 2: Simulate SSE message when at bottom
    messagesDiv.scrollTop = messagesDiv.scrollHeight; // Ensure at bottom

    // Add a test message
    const testMessage1 = `
        <div class="my-2 flex justify-start" id="assistant-test1">
            <div class="max-w-[70%] p-3 rounded-lg bg-gray-200">
                Test message 1 - should auto-scroll
            </div>
        </div>
    `;
    messagesDiv.insertAdjacentHTML('beforeend', testMessage1);

    // Trigger SSE event
    const sseEvent1 = new CustomEvent('htmx:sseMessage', {
        detail: { /* event details */ }
    });
    document.body.dispatchEvent(sseEvent1);

    // Wait for scroll animation
    await new Promise(resolve => setTimeout(resolve, 100));

    const afterFirstMessage = Math.abs(messagesDiv.scrollHeight - messagesDiv.scrollTop - messagesDiv.clientHeight) < 10;
    results.push({
        test: 'Auto-scrolls when at bottom',
        passed: afterFirstMessage,
        scrollTop: messagesDiv.scrollTop,
        scrollHeight: messagesDiv.scrollHeight
    });

    // Test 3: Scroll up to read, then add message (should NOT auto-scroll)
    messagesDiv.scrollTop = messagesDiv.scrollHeight / 2; // Scroll to middle
    const midPosition = messagesDiv.scrollTop;

    // Add another test message
    const testMessage2 = `
        <div class="my-2 flex justify-start" id="assistant-test2">
            <div class="max-w-[70%] p-3 rounded-lg bg-gray-200">
                Test message 2 - should NOT auto-scroll
            </div>
        </div>
    `;
    messagesDiv.insertAdjacentHTML('beforeend', testMessage2);

    // Trigger SSE event
    const sseEvent2 = new CustomEvent('htmx:sseMessage', {
        detail: { /* event details */ }
    });
    document.body.dispatchEvent(sseEvent2);

    // Wait for any potential scroll
    await new Promise(resolve => setTimeout(resolve, 100));

    // Position should be preserved (with small tolerance for layout changes)
    const positionPreserved = Math.abs(messagesDiv.scrollTop - midPosition) < 50;
    results.push({
        test: 'Preserves position when scrolled up',
        passed: positionPreserved,
        previousPosition: midPosition,
        currentPosition: messagesDiv.scrollTop,
        difference: Math.abs(messagesDiv.scrollTop - midPosition)
    });

    // Test 4: Test near-bottom threshold (within 100px)
    messagesDiv.scrollTop = messagesDiv.scrollHeight - messagesDiv.clientHeight - 50; // 50px from bottom

    const testMessage3 = `
        <div class="my-2 flex justify-start" id="assistant-test3">
            <div class="max-w-[70%] p-3 rounded-lg bg-gray-200">
                Test message 3 - within threshold, should auto-scroll
            </div>
        </div>
    `;
    messagesDiv.insertAdjacentHTML('beforeend', testMessage3);

    const sseEvent3 = new CustomEvent('htmx:sseMessage', {
        detail: { /* event details */ }
    });
    document.body.dispatchEvent(sseEvent3);

    await new Promise(resolve => setTimeout(resolve, 100));

    const nearBottomScrolled = Math.abs(messagesDiv.scrollHeight - messagesDiv.scrollTop - messagesDiv.clientHeight) < 10;
    results.push({
        test: 'Auto-scrolls when within 100px threshold',
        passed: nearBottomScrolled,
        scrollTop: messagesDiv.scrollTop,
        scrollHeight: messagesDiv.scrollHeight
    });

    // Test 5: Rapid message updates
    messagesDiv.scrollTop = messagesDiv.scrollHeight; // Start at bottom

    for (let i = 0; i < 5; i++) {
        const rapidMessage = `
            <div class="my-2 flex justify-start" id="assistant-rapid${i}">
                <div class="max-w-[70%] p-3 rounded-lg bg-gray-200">
                    Rapid message ${i}
                </div>
            </div>
        `;
        messagesDiv.insertAdjacentHTML('beforeend', rapidMessage);

        const rapidEvent = new CustomEvent('htmx:sseMessage', {
            detail: { /* event details */ }
        });
        document.body.dispatchEvent(rapidEvent);

        await new Promise(resolve => setTimeout(resolve, 20));
    }

    const afterRapidMessages = Math.abs(messagesDiv.scrollHeight - messagesDiv.scrollTop - messagesDiv.clientHeight) < 10;
    results.push({
        test: 'Handles rapid message updates',
        passed: afterRapidMessages,
        finalScrollTop: messagesDiv.scrollTop,
        finalScrollHeight: messagesDiv.scrollHeight
    });

    // Summary
    const allPassed = results.every(r => r.passed !== false);
    return {
        success: allPassed,
        results: results,
        summary: `${results.filter(r => r.passed).length}/${results.length} tests passed`
    };
}

// Export for Playwright
if (typeof module !== 'undefined' && module.exports) {
    module.exports = { testSSEScrollBehavior };
}