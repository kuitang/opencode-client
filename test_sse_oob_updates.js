// Test SSE OOB updates for file stats and dropdown
async function testSSECodeUpdates(page) {
    console.log('=== Testing SSE Code Tab Updates ===');

    // 1. Navigate to app
    await page.goto('http://localhost:8080');
    console.log('Navigated to app');

    // 2. Go to Code tab
    await page.click('button:has-text("Code")');
    await page.waitForTimeout(500);
    console.log('Switched to Code tab');

    // 3. Record initial state
    const initialFiles = await page.textContent('#file-count-container p:first-child');
    const initialLines = await page.textContent('#line-count-container p:first-child');
    const initialOptions = await page.locator('#file-selector option').count();
    console.log(`Initial state: ${initialFiles} files, ${initialLines} lines, ${initialOptions} options`);

    // 4. Create files directly in Docker to simulate OpenCode activity
    console.log('Creating test files in container...');
    const { exec } = require('child_process');
    const util = require('util');
    const execPromise = util.promisify(exec);

    // Get container name
    const { stdout: containers } = await execPromise('docker ps --format "{{.Names}}" | grep opencode-sandbox');
    const containerName = containers.trim().split('\n')[0];
    console.log(`Using container: ${containerName}`);

    // Create test files
    await execPromise(`docker exec ${containerName} sh -c "cd /app && echo 'test content' > sse_test1.txt && echo 'def foo(): pass' > sse_test2.py"`);
    console.log('Test files created');

    // 5. Trigger SSE update by simulating step completion
    // We need to send a message that will trigger file detection
    await page.fill('#message-input', 'List the files in the current directory');
    await page.click('button[type="submit"]');
    console.log('Sent message to trigger file scan');

    // 6. Wait for SSE updates (watch for changes in file count)
    console.log('Waiting for SSE updates...');
    await page.waitForFunction(
        (initial) => {
            const current = document.querySelector('#file-count-container p:first-child').textContent;
            return current !== initial;
        },
        initialFiles,
        { timeout: 10000 }
    );

    // 7. Verify all updates occurred
    const newFiles = await page.textContent('#file-count-container p:first-child');
    const newLines = await page.textContent('#line-count-container p:first-child');
    const newOptions = await page.locator('#file-selector option').count();

    console.log(`Updated state: ${newFiles} files, ${newLines} lines, ${newOptions} options`);

    // Assertions
    const filesIncreased = parseInt(newFiles) > parseInt(initialFiles);
    const linesIncreased = parseInt(newLines) >= parseInt(initialLines);
    const optionsIncreased = newOptions > initialOptions;

    console.log(`Files increased: ${filesIncreased}`);
    console.log(`Lines increased or same: ${linesIncreased}`);
    console.log(`Options increased: ${optionsIncreased}`);

    // 8. Check specific files appear in dropdown
    const optionTexts = await page.locator('#file-selector option').allTextContents();
    const hasSSETest1 = optionTexts.some(text => text.includes('sse_test1.txt'));
    const hasSSETest2 = optionTexts.some(text => text.includes('sse_test2.py'));

    console.log(`Dropdown contains sse_test1.txt: ${hasSSETest1}`);
    console.log(`Dropdown contains sse_test2.py: ${hasSSETest2}`);

    // 9. Verify regular messages still work
    const messageCount = await page.locator('.message').count();
    console.log(`Message count: ${messageCount}`);

    // Clean up test files
    await execPromise(`docker exec ${containerName} sh -c "cd /app && rm -f sse_test1.txt sse_test2.py"`);
    console.log('Test files cleaned up');

    // Summary
    const allPassed = filesIncreased && linesIncreased && optionsIncreased && hasSSETest1 && hasSSETest2;
    console.log(`\n=== Test ${allPassed ? 'PASSED' : 'FAILED'} ===`);

    return allPassed;
}

// Export for use in Playwright
if (typeof module !== 'undefined' && module.exports) {
    module.exports = { testSSECodeUpdates };
}