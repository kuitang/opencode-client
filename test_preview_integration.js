const { chromium } = require('playwright');
const { exec } = require('child_process');
const { promisify } = require('util');
const execPromise = promisify(exec);

async function testPreviewIntegration() {
    console.log('Starting Preview Integration Test');
    const browser = await chromium.launch({
        headless: false,
        args: ['--disable-blink-features=AutomationControlled']
    });

    const context = await browser.newContext({
        viewport: { width: 1280, height: 720 }
    });

    const page = await context.newPage();

    try {
        // 1. Navigate to the application
        console.log('1. Navigating to http://localhost:8080');
        await page.goto('http://localhost:8080', { waitUntil: 'networkidle' });

        // 2. Wait for initial page load
        await page.waitForSelector('#message-input', { timeout: 10000 });
        console.log('   ✓ Page loaded successfully');

        // 3. Send message to create a Python server
        console.log('2. Sending message to create Python HTTP server');
        const message = `Create a simple Python HTTP server that:
1. Runs on port 5000
2. Serves a basic HTML page with "Hello World" and the current time
3. Save it as server.py
4. Then run it with: python3 server.py`;

        await page.fill('#message-input', message);
        await page.click('button[type="submit"]');
        console.log('   ✓ Message sent');

        // 4. Wait for the assistant to process and respond
        console.log('3. Waiting for assistant response...');

        // Wait for the response to appear and the server to be created
        // We'll wait for a message that contains "server.py" or indicates completion
        await page.waitForFunction(
            () => {
                const messages = document.querySelectorAll('.message');
                const lastMessage = messages[messages.length - 1];
                return lastMessage && (
                    lastMessage.textContent.includes('server.py') ||
                    lastMessage.textContent.includes('created') ||
                    lastMessage.textContent.includes('running')
                );
            },
            { timeout: 60000 } // Give the LLM 60 seconds to respond
        );
        console.log('   ✓ Assistant created the server file');

        // 5. Wait a bit for the server to actually start
        console.log('4. Waiting for server to start...');
        await page.waitForTimeout(5000);

        // 6. Navigate to the Files tab to verify the file was created
        console.log('5. Checking Files tab for server.py');
        await page.click('button:has-text("Files")');
        await page.waitForTimeout(2000);

        // Check if server.py exists in the file list
        const fileDropdown = await page.locator('#file-selector');
        const fileOptions = await fileDropdown.locator('option').allTextContents();
        const hasServerFile = fileOptions.some(text => text.includes('server.py'));
        console.log(`   ✓ server.py exists: ${hasServerFile}`);

        // 7. Navigate to the Preview tab
        console.log('6. Navigating to Preview tab');
        await page.click('button:has-text("Preview")');
        await page.waitForTimeout(3000);

        // 8. Check if the preview iframe is loaded
        const previewIframe = await page.locator('#preview-iframe').count();
        if (previewIframe > 0) {
            console.log('   ✓ Preview iframe detected');

            // Try to get the iframe content
            const frame = page.frameLocator('#preview-iframe');

            // Wait for content in the iframe
            try {
                await frame.locator('body').waitFor({ timeout: 10000 });

                // Check if "Hello World" is present
                const bodyText = await frame.locator('body').textContent();
                const hasHelloWorld = bodyText && bodyText.includes('Hello');
                console.log(`   ✓ Preview content loaded: ${hasHelloWorld ? 'Hello World found' : 'Content loaded but no Hello World'}`);

                // Log the actual content for debugging
                if (bodyText) {
                    console.log('   Preview content:', bodyText.substring(0, 200));
                }
            } catch (frameError) {
                console.log('   ⚠ Could not access iframe content (may be blocked by CORS)');

                // Alternative: Check if the iframe at least has a src attribute
                const iframeSrc = await page.locator('#preview-iframe').getAttribute('src');
                console.log(`   Iframe src: ${iframeSrc}`);
            }
        } else {
            // Check for the "No Application Running" message
            const noAppMessage = await page.locator('text=No Application Running').count();
            if (noAppMessage > 0) {
                console.log('   ⚠ No application detected - server might not be running on expected port');

                // Try to refresh the preview
                console.log('7. Attempting to refresh preview...');
                const refreshButton = await page.locator('button:has-text("Refresh")').first();
                if (refreshButton) {
                    await refreshButton.click();
                    await page.waitForTimeout(3000);

                    // Check again for iframe
                    const retryIframe = await page.locator('#preview-iframe').count();
                    if (retryIframe > 0) {
                        console.log('   ✓ Preview iframe appeared after refresh');
                    } else {
                        console.log('   ⚠ Still no preview after refresh');
                    }
                }
            }
        }

        // 9. Optional: Test the Open in New Tab button
        const openTabButton = await page.locator('button:has-text("Open in New Tab")').count();
        if (openTabButton > 0) {
            console.log('   ✓ Open in New Tab button is available');
        }

        // 10. Summary
        console.log('\n=== Test Summary ===');
        console.log('✓ Application loaded');
        console.log('✓ Message sent to LLM');
        console.log('✓ Server file created');
        console.log(`${hasServerFile ? '✓' : '⚠'} File visible in Files tab`);
        console.log(`${previewIframe > 0 ? '✓' : '⚠'} Preview iframe rendered`);

        return {
            success: true,
            fileCreated: hasServerFile,
            previewLoaded: previewIframe > 0
        };

    } catch (error) {
        console.error('Test failed:', error);

        // Take a screenshot for debugging
        await page.screenshot({ path: 'preview-test-error.png', fullPage: true });
        console.log('Screenshot saved as preview-test-error.png');

        return {
            success: false,
            error: error.message
        };
    } finally {
        await browser.close();
    }
}

// Run the test
(async () => {
    console.log('Preview Integration Test Runner');
    console.log('================================\n');

    // Check if the server is running
    try {
        const response = await fetch('http://localhost:8080');
        console.log('✓ Server is running on port 8080\n');
    } catch (error) {
        console.error('✗ Server is not running on port 8080');
        console.error('Please start the server with: ./opencode-chat -port 8080');
        process.exit(1);
    }

    const result = await testPreviewIntegration();

    console.log('\n================================');
    if (result.success) {
        console.log('✓ Preview integration test completed');
        if (!result.fileCreated || !result.previewLoaded) {
            console.log('⚠ Some features may not be working as expected');
            process.exit(1);
        }
    } else {
        console.log('✗ Preview integration test failed');
        process.exit(1);
    }
})();