// Playwright test script for terminal functionality
// Run this after starting the opencode-chat server

async function testTerminalFunctionality(page) {
    console.log("Starting terminal functionality tests...");

    // Test 1: Navigate to the application
    console.log("Test 1: Navigating to application...");
    await page.goto('http://localhost:8090');
    await page.waitForLoadState('domcontentloaded');
    console.log("✓ Application loaded");

    // Test 2: Click on Terminal tab
    console.log("Test 2: Clicking Terminal tab...");
    await page.getByRole('button', { name: 'Terminal' }).click();
    await page.waitForTimeout(2000); // Wait for terminal to load
    console.log("✓ Terminal tab clicked");

    // Test 3: Check if terminal iframe is present
    console.log("Test 3: Checking terminal iframe...");
    const terminalIframe = await page.locator('#terminal-iframe');
    const isVisible = await terminalIframe.isVisible();
    if (isVisible) {
        console.log("✓ Terminal iframe is visible");
    } else {
        console.log("✗ Terminal iframe is not visible");
        return;
    }

    // Test 4: Direct navigation to gotty
    console.log("Test 4: Testing direct gotty access...");
    await page.goto('http://localhost:41467');
    await page.waitForLoadState('domcontentloaded');

    const title = await page.title();
    if (title.includes('GoTTY')) {
        console.log("✓ Gotty is accessible directly");
    } else {
        console.log("✗ Gotty is not accessible");
        return;
    }

    // Test 5: Try to interact with terminal
    console.log("Test 5: Testing terminal interaction...");

    // Click on the terminal to focus
    const frame = page.frameLocator('iframe');
    const terminal = frame.locator('x-screen');
    await terminal.click();

    // Try typing a simple command
    await page.keyboard.type('echo "Hello from terminal"');
    await page.keyboard.press('Enter');
    await page.waitForTimeout(1000);

    // Take a screenshot for verification
    await page.screenshot({ path: 'terminal-test.png' });
    console.log("✓ Screenshot saved as terminal-test.png");

    console.log("\nTerminal tests completed!");
}

async function testFileSync(page) {
    console.log("\nStarting file synchronization tests...");

    // Test 1: Create a file through terminal
    console.log("Test 1: Creating file via terminal...");
    await page.goto('http://localhost:41467');
    await page.waitForLoadState('domcontentloaded');

    const frame = page.frameLocator('iframe');
    const terminal = frame.locator('x-screen');
    await terminal.click();

    await page.keyboard.type('echo "Test content" > test.txt');
    await page.keyboard.press('Enter');
    await page.waitForTimeout(1000);

    await page.keyboard.type('cat test.txt');
    await page.keyboard.press('Enter');
    await page.waitForTimeout(1000);

    console.log("✓ File created via terminal");

    // Test 2: Check if file appears in Code tab
    console.log("Test 2: Checking Code tab for new file...");
    await page.goto('http://localhost:8090');
    await page.getByRole('button', { name: 'Code' }).click();
    await page.waitForTimeout(2000);

    const codeContent = await page.locator('#main-content').textContent();
    console.log("Code tab content includes:", codeContent.includes('test.txt') ? 'test.txt' : 'file not found');

    console.log("\nFile sync tests completed!");
}

async function testHtop(page) {
    console.log("\nStarting htop interactive test...");

    await page.goto('http://localhost:41467');
    await page.waitForLoadState('domcontentloaded');

    const frame = page.frameLocator('iframe');
    const terminal = frame.locator('x-screen');
    await terminal.click();

    // Run htop
    console.log("Running htop...");
    await page.keyboard.type('htop');
    await page.keyboard.press('Enter');
    await page.waitForTimeout(3000);

    // Take screenshot of htop
    await page.screenshot({ path: 'htop-test.png' });
    console.log("✓ Htop screenshot saved as htop-test.png");

    // Exit htop
    await page.keyboard.press('q');
    await page.waitForTimeout(1000);

    console.log("✓ Htop test completed!");
}

// Export functions for use with Playwright MCP
if (typeof module !== 'undefined' && module.exports) {
    module.exports = {
        testTerminalFunctionality,
        testFileSync,
        testHtop
    };
}

// Instructions:
// 1. Start the opencode-chat server: ./opencode-chat -port 8090
// 2. Wait for the server to start and display the gotty port
// 3. Update the gotty port in the script (currently using 41467)
// 4. Run tests with Playwright MCP or standalone Playwright