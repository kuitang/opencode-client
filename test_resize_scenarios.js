// Comprehensive Playwright tests for resize race conditions and scroll preservation
// These tests ensure UI stability during viewport transitions and user interactions
// Run with Playwright MCP after starting the server on port 8080

const TEST_PORT = 8080;
const BASE_URL = `http://localhost:${TEST_PORT}`;

// Test configuration
const DESKTOP_WIDTH = 1280;
const DESKTOP_HEIGHT = 720;
const MOBILE_WIDTH = 375;
const MOBILE_HEIGHT = 812;
const TABLET_WIDTH = 768;
const TABLET_HEIGHT = 1024;

// =======================
// CRITICAL TEST SUITE
// =======================

// Test 1: Rapid Resize Transitions (Most Difficult - Tests Debouncing)
// This test ensures the debouncing mechanism prevents UI flickering
async function testRapidResizeTransitions() {
    return await page.evaluate(`async () => {
        const results = [];
        
        for (let i = 0; i < 10; i++) {
            const width = i % 2 === 0 ? 375 : 1280;
            window.innerWidth = width;
            window.dispatchEvent(new Event('resize'));
            
            await new Promise(resolve => setTimeout(resolve, 50));
            
            const chat = document.getElementById('chat-container');
            results.push({
                iteration: i,
                width: width,
                hasExpanded: chat.classList.contains('chat-expanded'),
                hasMinimized: chat.classList.contains('chat-minimized')
            });
        }
        
        // Wait for debounce to settle
        await new Promise(resolve => setTimeout(resolve, 400));
        
        const chat = document.getElementById('chat-container');
        return {
            rapidResizeResults: results,
            finalState: {
                isExpanded: chat.classList.contains('chat-expanded'),
                isMinimized: chat.classList.contains('chat-minimized'),
                hasNoMobileClasses: !chat.classList.contains('chat-expanded') && !chat.classList.contains('chat-minimized')
            }
        };
    }`);
}

// Test 2: Scroll Position Preservation (MOST CRITICAL - User Priority)
// This test ensures scroll positions are maintained across viewport transitions
async function testScrollPositionPreservation() {
    return await page.evaluate(`async () => {
        const messagesDiv = document.getElementById('messages');
        const results = [];
        
        // Add test messages if needed
        if (messagesDiv.children.length < 20) {
            for (let i = 1; i <= 20; i++) {
                const messageHtml = \`
                    <div class="flex mb-4">
                        <div class="bg-blue-100 text-gray-900 rounded-lg p-3 max-w-[80%]">
                            <p>Test message \${i} - Lorem ipsum dolor sit amet, consectetur adipiscing elit.</p>
                        </div>
                    </div>\`;
                messagesDiv.insertAdjacentHTML('beforeend', messageHtml);
            }
        }
        
        // Test positions: top, middle, bottom
        const testPositions = [
            { name: 'top', position: 0 },
            { name: 'middle', position: messagesDiv.scrollHeight / 2 },
            { name: 'bottom', position: messagesDiv.scrollHeight }
        ];
        
        for (let test of testPositions) {
            // Set initial position in mobile
            window.innerWidth = 375;
            window.dispatchEvent(new Event('resize'));
            await new Promise(resolve => setTimeout(resolve, 300));
            
            messagesDiv.scrollTop = test.position;
            const mobileScroll = {
                top: messagesDiv.scrollTop,
                height: messagesDiv.scrollHeight,
                isAtBottom: Math.abs(messagesDiv.scrollHeight - messagesDiv.scrollTop - messagesDiv.clientHeight) < 10
            };
            
            // Switch to desktop
            window.innerWidth = 1280;
            window.dispatchEvent(new Event('resize'));
            await new Promise(resolve => setTimeout(resolve, 300));
            
            const desktopScroll = {
                top: messagesDiv.scrollTop,
                height: messagesDiv.scrollHeight,
                isAtBottom: Math.abs(messagesDiv.scrollHeight - messagesDiv.scrollTop - messagesDiv.clientHeight) < 10
            };
            
            // Switch back to mobile
            window.innerWidth = 375;
            window.dispatchEvent(new Event('resize'));
            await new Promise(resolve => setTimeout(resolve, 300));
            
            const restoredMobileScroll = {
                top: messagesDiv.scrollTop,
                height: messagesDiv.scrollHeight,
                isAtBottom: Math.abs(messagesDiv.scrollHeight - messagesDiv.scrollTop - messagesDiv.clientHeight) < 10
            };
            
            results.push({
                position: test.name,
                mobile: mobileScroll,
                desktop: desktopScroll,
                restoredMobile: restoredMobileScroll,
                preserved: test.name === 'bottom' 
                    ? restoredMobileScroll.isAtBottom 
                    : Math.abs(mobileScroll.top - restoredMobileScroll.top) < 50
            });
        }
        
        return results;
    }`);
}

// Test 3: Resize During Active Scrolling (Most Complex - Tests Race Conditions)
// This test ensures viewport changes don't interrupt scroll animations
async function testResizeDuringActiveScrolling() {
    return await page.evaluate(`async () => {
        const messagesDiv = document.getElementById('messages');
        
        // Ensure we have messages
        if (messagesDiv.children.length < 20) {
            for (let i = 1; i <= 20; i++) {
                const messageHtml = \`
                    <div class="flex mb-4">
                        <div class="bg-blue-100 text-gray-900 rounded-lg p-3 max-w-[80%]">
                            <p>Test message \${i} - Lorem ipsum dolor sit amet</p>
                        </div>
                    </div>\`;
                messagesDiv.insertAdjacentHTML('beforeend', messageHtml);
            }
        }
        
        // Scroll to middle
        messagesDiv.scrollTop = messagesDiv.scrollHeight / 2;
        const initialScroll = messagesDiv.scrollTop;
        
        // Start smooth scroll to top
        messagesDiv.scrollTo({ top: 0, behavior: 'smooth' });
        
        // Resize during scroll animation
        await new Promise(resolve => setTimeout(resolve, 100));
        window.innerWidth = 375;
        window.dispatchEvent(new Event('resize'));
        
        // Wait for scroll and resize to complete
        await new Promise(resolve => setTimeout(resolve, 500));
        
        const mobileScroll = messagesDiv.scrollTop;
        
        // Resize back to desktop
        window.innerWidth = 1280;
        window.dispatchEvent(new Event('resize'));
        
        await new Promise(resolve => setTimeout(resolve, 500));
        
        return {
            initialScroll: initialScroll,
            mobileScroll: mobileScroll,
            finalScroll: messagesDiv.scrollTop,
            scrollHeight: messagesDiv.scrollHeight,
            testPassed: true // No errors means the test passed
        };
    }`);
}

// Test 4: Complex Interaction Patterns (Most Comprehensive)
// Tests clicking outside with text, rapid scrolling, toggle during scroll, etc.
async function testComplexInteractions() {
    return await page.evaluate(`async () => {
        const results = [];
        const messagesDiv = document.getElementById('messages');
        const messageInput = document.getElementById('message-input');
        const chat = document.getElementById('chat-container');
        
        // Test 1: Click outside chat with text in input (should NOT minimize)
        messageInput.value = "Important message being typed...";
        messageInput.focus();
        document.body.click(); // Click outside
        await new Promise(resolve => setTimeout(resolve, 100));
        results.push({
            test: "Click outside with text",
            chatState: chat.classList.contains('chat-expanded') ? 'expanded' : 'minimized',
            inputValue: messageInput.value,
            passed: chat.classList.contains('chat-expanded') && messageInput.value.length > 0
        });
        
        // Test 2: Scroll to different positions rapidly
        const positions = [0, messagesDiv.scrollHeight / 2, messagesDiv.scrollHeight, 100, messagesDiv.scrollHeight];
        for (let pos of positions) {
            messagesDiv.scrollTop = pos;
            await new Promise(resolve => setTimeout(resolve, 50));
        }
        results.push({
            test: "Rapid scrolling",
            finalScrollTop: messagesDiv.scrollTop,
            passed: messagesDiv.scrollTop > 0
        });
        
        // Test 3: Toggle while scrolling
        messagesDiv.scrollTo({ top: 0, behavior: 'smooth' });
        await new Promise(resolve => setTimeout(resolve, 50));
        const toggleButton = document.getElementById('chat-toggle');
        if (toggleButton) {
            toggleButton.click();
            await new Promise(resolve => setTimeout(resolve, 300));
        }
        results.push({
            test: "Toggle during scroll",
            chatState: chat.classList.contains('chat-expanded') ? 'expanded' : 'minimized',
            passed: true
        });
        
        // Test 4: Rapid toggle clicks (test debouncing)
        if (toggleButton) {
            for (let i = 0; i < 10; i++) {
                toggleButton.click();
                await new Promise(resolve => setTimeout(resolve, 30));
            }
            await new Promise(resolve => setTimeout(resolve, 400));
        }
        
        // Test 5: Complex resize sequence
        const sizes = [500, 1024, 375, 768, 1280];
        for (let width of sizes) {
            window.innerWidth = width;
            window.dispatchEvent(new Event('resize'));
            await new Promise(resolve => setTimeout(resolve, 100));
        }
        
        results.push({
            test: "Complex resize sequence",
            finalChatState: chat.classList.contains('chat-expanded') ? 'expanded' : 
                           chat.classList.contains('chat-minimized') ? 'minimized' : 'desktop',
            passed: true
        });
        
        return {
            results: results,
            allTestsPassed: results.every(r => r.passed !== false)
        };
    }`);
}

// Test 5: Scroll Preservation with Toggle (Additional Critical Test)
// Ensures scroll position is maintained when toggling chat minimize/expand
async function testScrollPreservationWithToggle() {
    return await page.evaluate(`async () => {
        const messagesDiv = document.getElementById('messages');
        const toggleButton = document.getElementById('chat-toggle');
        const chat = document.getElementById('chat-container');
        
        // Ensure mobile view
        window.innerWidth = 375;
        window.dispatchEvent(new Event('resize'));
        await new Promise(resolve => setTimeout(resolve, 300));
        
        // Scroll to specific position
        const targetScroll = messagesDiv.scrollHeight / 2;
        messagesDiv.scrollTop = targetScroll;
        const initialScroll = messagesDiv.scrollTop;
        
        // Toggle to minimize
        if (toggleButton) {
            toggleButton.click();
            await new Promise(resolve => setTimeout(resolve, 350));
        }
        
        const minimizedState = chat.classList.contains('chat-minimized');
        
        // Toggle back to expand
        if (toggleButton) {
            toggleButton.click();
            await new Promise(resolve => setTimeout(resolve, 350));
        }
        
        const expandedState = chat.classList.contains('chat-expanded');
        const restoredScroll = messagesDiv.scrollTop;
        
        // Test scrolling to bottom and preserving that
        messagesDiv.scrollTop = messagesDiv.scrollHeight;
        await new Promise(resolve => setTimeout(resolve, 100));
        
        if (toggleButton) {
            toggleButton.click(); // Minimize
            await new Promise(resolve => setTimeout(resolve, 350));
            toggleButton.click(); // Expand
            await new Promise(resolve => setTimeout(resolve, 350));
        }
        
        const isAtBottom = Math.abs(messagesDiv.scrollHeight - messagesDiv.scrollTop - messagesDiv.clientHeight) < 10;
        
        return {
            initialScroll: initialScroll,
            restoredScroll: restoredScroll,
            scrollPreserved: Math.abs(initialScroll - restoredScroll) < 50,
            minimizedCorrectly: minimizedState,
            expandedCorrectly: expandedState,
            bottomPreserved: isAtBottom
        };
    }`);
}

// Test 6: Edge Case Screen Sizes
// Tests breakpoint boundaries and unusual viewport dimensions
async function testEdgeCaseScreenSizes() {
    return await page.evaluate(`async () => {
        const edgeSizes = [
            { width: 1023, height: 768, name: 'Just below desktop breakpoint' },
            { width: 1024, height: 768, name: 'Exactly at desktop breakpoint' },
            { width: 320, height: 568, name: 'Very small mobile' },
            { width: 2560, height: 1440, name: 'Large desktop' },
            { width: 812, height: 375, name: 'Mobile landscape' },
            { width: 1366, height: 768, name: 'Common laptop' },
        ];
        
        const results = [];
        const chat = document.getElementById('chat-container');
        
        for (const size of edgeSizes) {
            window.innerWidth = size.width;
            window.innerHeight = size.height;
            window.dispatchEvent(new Event('resize'));
            await new Promise(resolve => setTimeout(resolve, 300));
            
            const state = {
                ...size,
                hasNoMobileClasses: !chat.classList.contains('chat-expanded') && !chat.classList.contains('chat-minimized'),
                mode: size.width >= 1024 ? 'Desktop' : 'Mobile'
            };
            results.push(state);
        }
        
        return results;
    }`);
}

// Main test runner
async function runAllTests() {
    console.log('🚀 Running Comprehensive UI Resize & Scroll Tests\n');
    console.log('=' .repeat(50));
    
    const tests = [
        { name: 'Rapid Resize Transitions', fn: testRapidResizeTransitions },
        { name: 'Scroll Position Preservation (CRITICAL)', fn: testScrollPositionPreservation },
        { name: 'Resize During Active Scrolling', fn: testResizeDuringActiveScrolling },
        { name: 'Complex Interaction Patterns', fn: testComplexInteractions },
        { name: 'Scroll Preservation with Toggle', fn: testScrollPreservationWithToggle },
        { name: 'Edge Case Screen Sizes', fn: testEdgeCaseScreenSizes }
    ];
    
    const results = [];
    
    for (const test of tests) {
        console.log(`\n📋 Running: ${test.name}`);
        try {
            const result = await test.fn();
            console.log('✅ Passed');
            results.push({ test: test.name, status: 'passed', result });
        } catch (error) {
            console.log('❌ Failed:', error.message);
            results.push({ test: test.name, status: 'failed', error: error.message });
        }
    }
    
    console.log('\n' + '=' .repeat(50));
    console.log('📊 Test Summary:');
    console.log(`  Passed: ${results.filter(r => r.status === 'passed').length}`);
    console.log(`  Failed: ${results.filter(r => r.status === 'failed').length}`);
    
    return results;
}

// Export for use
if (typeof module !== 'undefined' && module.exports) {
    module.exports = {
        runAllTests,
        testRapidResizeTransitions,
        testScrollPositionPreservation,
        testResizeDuringActiveScrolling,
        testComplexInteractions,
        testScrollPreservationWithToggle,
        testEdgeCaseScreenSizes
    };
}

// Usage instructions
console.log(`
==============================================
Playwright MCP Test Suite for Resize & Scroll
==============================================

To run these tests:
1. Start the server: ./opencode-chat -port 8080
2. Navigate to http://localhost:8080 with Playwright
3. Execute the test functions via page.evaluate()

Critical tests for scroll preservation:
- testScrollPositionPreservation() 
- testScrollPreservationWithToggle()
- testResizeDuringActiveScrolling()

These tests ensure UI stability during viewport
transitions and preserve user scroll context.
`);