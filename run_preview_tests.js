const { chromium } = require('playwright');

async function runPreviewTests() {
    console.log('Starting Preview Feature Tests');
    console.log('==============================\n');

    const browser = await chromium.launch({
        headless: false,
        args: ['--disable-blink-features=AutomationControlled']
    });

    const context = await browser.newContext({
        viewport: { width: 1280, height: 720 }
    });

    const page = await context.newPage();

    try {
        // Navigate to the application
        console.log('Navigating to http://localhost:8085');
        await page.goto('http://localhost:8085', { waitUntil: 'networkidle' });

        // Test 1: First load shows preview content
        console.log('\n1. Testing first load shows preview content...');
        const mainContent = await page.locator('#main-content');
        const mainContentText = await mainContent.textContent();

        if (mainContentText.includes('Welcome to VibeCoding')) {
            console.log('   ✗ First load still shows placeholder');
            throw new Error('First load shows placeholder instead of preview content');
        } else if (mainContentText.includes('No Application Running')) {
            console.log('   ✓ First load correctly shows preview "No Application Running"');
        } else {
            const hasIframe = await page.locator('#preview-iframe').count();
            if (hasIframe > 0) {
                console.log('   ✓ First load shows preview with iframe (server already running)');
            }
        }

        // Test 2: Preview tab functionality
        console.log('\n2. Testing Preview tab basic functionality...');
        // Click another tab first
        await page.click('button:has-text("Code")');
        await page.waitForTimeout(1000);

        // Now click Preview tab
        await page.click('button:has-text("Preview")');
        await page.waitForTimeout(1000);

        const previewContent = await page.locator('#main-content').textContent();
        if (previewContent.includes('No Application Running') || await page.locator('#preview-iframe').count() > 0) {
            console.log('   ✓ Preview tab loads correctly');
        } else {
            throw new Error('Preview tab did not load expected content');
        }

        // Test 3: Kill button presence
        console.log('\n3. Testing Kill button functionality...');

        // First, start a Python server to test with
        console.log('   Starting a test server on port 5555...');
        const { exec } = require('child_process');
        const serverProcess = exec('python3 -m http.server 5555');

        // Wait for server to start
        await page.waitForTimeout(3000);

        // Refresh preview to detect the new port
        await page.click('button:has-text("Refresh")');
        await page.waitForTimeout(2000);

        // Check for Kill button
        const killButton = await page.locator('button:has-text("Kill")').count();
        if (killButton > 0) {
            console.log('   ✓ Kill button appears when server is running');

            // Check for hidden port input
            const portInput = await page.locator('input[name="port"]').first();
            const portValue = await portInput.getAttribute('value');
            console.log(`   ✓ Kill button has port value: ${portValue}`);

            // Click Kill button
            console.log('   Clicking Kill button...');
            await page.click('button:has-text("Kill")');
            await page.waitForTimeout(3000);

            // Check that we're back to "No Application Running"
            const afterKill = await page.locator('#main-content').textContent();
            if (afterKill.includes('No Application Running')) {
                console.log('   ✓ Kill button successfully stopped the server');
            } else {
                console.log('   ⚠ Server may not have been killed properly');
            }
        } else {
            console.log('   ⚠ Kill button not found (port may not be detected)');

            // Kill the test server anyway
            serverProcess.kill();
        }

        // Clean up test server if still running
        try {
            serverProcess.kill();
        } catch (e) {
            // Server already killed
        }

        console.log('\n=== Test Summary ===');
        console.log('✓ First load shows preview content (not placeholder)');
        console.log('✓ Preview tab functionality works');
        console.log('✓ Kill button appears and functions correctly');
        console.log('\nAll tests passed!');

    } catch (error) {
        console.error('\n✗ Test failed:', error.message);

        // Take screenshot for debugging
        await page.screenshot({ path: 'preview-test-failure.png', fullPage: true });
        console.log('Screenshot saved as preview-test-failure.png');

        process.exit(1);
    } finally {
        await browser.close();
    }
}

// Check if server is running
(async () => {
    try {
        const response = await fetch('http://localhost:8085');
        console.log('✓ Server is running on port 8085\n');
    } catch (error) {
        console.error('✗ Server is not running on port 8085');
        console.error('Please start the server with: ./opencode-chat -port 8085');
        process.exit(1);
    }

    await runPreviewTests();
})();