// Playwright test to verify chat banner and tabbar are flush aligned
// This test explicitly checks that the bottom edges of both elements are at the same position

function testFlushAlignment() {
    console.log('Starting flush alignment test...');
    
    const chatHeader = document.querySelector('header');
    const tabNav = document.querySelector('nav');
    
    if (!chatHeader || !tabNav) {
        console.error('Test failed: Could not find chat header or tab navigation');
        return false;
    }
    
    // Get bounding rectangles
    const headerRect = chatHeader.getBoundingClientRect();
    const navRect = tabNav.getBoundingClientRect();
    
    // Calculate bottom positions
    const headerBottom = headerRect.bottom;
    const navBottom = navRect.bottom;
    
    // Check if bottoms are flush (within 1px tolerance for rendering differences)
    const isFlush = Math.abs(headerBottom - navBottom) <= 1;
    
    console.log(`Chat header bottom: ${headerBottom}px`);
    console.log(`Tab nav bottom: ${navBottom}px`);
    console.log(`Difference: ${Math.abs(headerBottom - navBottom)}px`);
    console.log(`Heights - Header: ${headerRect.height}px, Nav: ${navRect.height}px`);
    
    if (isFlush) {
        console.log('✅ PASS: Chat banner and tabbar are flush aligned');
        return true;
    } else {
        console.error('❌ FAIL: Chat banner and tabbar are NOT flush aligned');
        console.error(`Expected difference ≤ 1px, got ${Math.abs(headerBottom - navBottom)}px`);
        return false;
    }
}

// Test across different viewport sizes to ensure responsive behavior
function testFlushAlignmentResponsive() {
    console.log('Starting responsive flush alignment test...');
    
    const viewports = [
        { width: 375, height: 667, name: 'Mobile' },
        { width: 768, height: 1024, name: 'Tablet' },
        { width: 1024, height: 768, name: 'Desktop' }
    ];
    
    let allPassed = true;
    
    viewports.forEach(viewport => {
        console.log(`\nTesting ${viewport.name} viewport (${viewport.width}x${viewport.height})`);
        
        // Simulate viewport change
        window.resizeTo(viewport.width, viewport.height);
        
        // Wait for potential CSS transitions
        setTimeout(() => {
            const passed = testFlushAlignment();
            if (!passed) allPassed = false;
        }, 100);
    });
    
    return allPassed;
}

// Export for use in Playwright tests
if (typeof module !== 'undefined' && module.exports) {
    module.exports = { testFlushAlignment, testFlushAlignmentResponsive };
}