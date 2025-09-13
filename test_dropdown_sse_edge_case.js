// Playwright test for dropdown selection during SSE updates
// Tests edge case: selecting a file dropdown while sending a message

async function testDropdownSelectionDuringSSE(page) {
    console.log("Testing dropdown selection during SSE updates...");

    // Navigate to the application
    await page.goto('http://localhost:8080');

    // Wait for initial load
    await page.waitForSelector('#message-input');

    // Send a message that will create files
    await page.fill('#message-input', 'Create a simple hello.py file with print("Hello, World!")');
    await page.click('button[type="submit"]');

    // Wait for SSE to start processing
    await page.waitForTimeout(500);

    // Click on Code tab to see the dropdown
    await page.click('button:has-text("Code")');

    // Wait for dropdown to be visible
    await page.waitForSelector('#file-selector', { state: 'visible' });

    // Immediately focus the dropdown (this triggers manual refresh)
    await page.focus('#file-selector');

    // Check that dropdown is still focused after manual refresh
    const isDropdownFocused = await page.evaluate(() => {
        return document.activeElement?.id === 'file-selector';
    });

    console.log("Dropdown focused after manual refresh:", isDropdownFocused);

    // Wait for SSE updates to complete
    await page.waitForFunction(() => {
        const fileCount = document.querySelector('#file-count-container p');
        return fileCount && fileCount.textContent !== '0';
    }, { timeout: 10000 });

    // Check if dropdown is still focused after SSE update
    const stillFocused = await page.evaluate(() => {
        return document.activeElement?.id === 'file-selector';
    });

    console.log("Dropdown still focused after SSE update:", stillFocused);

    // Get the current selected value
    const selectedValue = await page.evaluate(() => {
        const select = document.querySelector('#file-selector');
        return select?.value || '';
    });

    console.log("Selected value after updates:", selectedValue);

    // Test selecting a file while more SSE updates come in
    const options = await page.$$eval('#file-selector option', opts =>
        opts.map(opt => ({ value: opt.value, text: opt.textContent }))
    );

    console.log("Available options:", options);

    if (options.length > 1) {
        // Select the first actual file (not placeholder)
        await page.selectOption('#file-selector', options[1].value);

        // Send another message to trigger more SSE updates
        await page.fill('#message-input', 'Create another file world.py with print("World!")');
        await page.click('button[type="submit"]');

        // Wait a bit for SSE to process
        await page.waitForTimeout(1000);

        // Check if our selection is preserved
        const preservedSelection = await page.evaluate(() => {
            const select = document.querySelector('#file-selector');
            return select?.value || '';
        });

        console.log("Selection preserved after new SSE update:", preservedSelection === options[1].value);
        console.log("Current selection:", preservedSelection);
    }

    return {
        dropdownFocusedAfterManualRefresh: isDropdownFocused,
        dropdownFocusedAfterSSE: stillFocused,
        selectionPreserved: selectedValue === '',
        finalOptions: options
    };
}

// Test rapid dropdown interactions during SSE
async function testRapidDropdownInteraction(page) {
    console.log("Testing rapid dropdown interactions during SSE...");

    await page.goto('http://localhost:8080');
    await page.waitForSelector('#message-input');

    // Send message to create multiple files
    await page.fill('#message-input', `
        Create three Python files:
        1. main.py with a main function
        2. utils.py with utility functions
        3. config.py with configuration
    `);
    await page.click('button[type="submit"]');

    // Navigate to Code tab
    await page.click('button:has-text("Code")');
    await page.waitForSelector('#file-selector', { state: 'visible' });

    // Rapidly focus/blur the dropdown while SSE updates are coming
    const interactions = [];
    for (let i = 0; i < 5; i++) {
        await page.focus('#file-selector');
        interactions.push({
            action: 'focus',
            timestamp: Date.now(),
            optionCount: await page.$$eval('#file-selector option', opts => opts.length)
        });

        await page.waitForTimeout(200);

        await page.blur('#file-selector');
        interactions.push({
            action: 'blur',
            timestamp: Date.now(),
            optionCount: await page.$$eval('#file-selector option', opts => opts.length)
        });

        await page.waitForTimeout(200);
    }

    console.log("Interaction log:", interactions);

    // Final state check
    const finalState = {
        optionCount: await page.$$eval('#file-selector option', opts => opts.length),
        selectedValue: await page.$eval('#file-selector', el => el.value),
        dropdownHTML: await page.$eval('#file-selector', el => el.innerHTML)
    };

    console.log("Final dropdown state:", finalState);

    return {
        interactions,
        finalState
    };
}

// Test manual refresh necessity
async function testManualRefreshNecessity(page) {
    console.log("Testing if manual refresh is still necessary...");

    await page.goto('http://localhost:8080');
    await page.waitForSelector('#message-input');

    // Create initial file
    await page.fill('#message-input', 'Create test1.txt with "initial content"');
    await page.click('button[type="submit"]');

    await page.waitForTimeout(2000); // Wait for SSE to complete

    // Go to Code tab
    await page.click('button:has-text("Code")');
    await page.waitForSelector('#file-selector', { state: 'visible' });

    // Get initial options
    const initialOptions = await page.$$eval('#file-selector option', opts =>
        opts.map(opt => opt.value)
    );

    console.log("Initial options:", initialOptions);

    // Create file outside of SSE (simulate external file creation)
    // This would need to be done through a different mechanism
    // For now, we'll test SSE-only updates

    // Create another file via message
    await page.fill('#message-input', 'Create test2.txt with "second content"');
    await page.click('button[type="submit"]');

    // Wait for SSE update without manual refresh
    await page.waitForTimeout(2000);

    const optionsAfterSSE = await page.$$eval('#file-selector option', opts =>
        opts.map(opt => opt.value)
    );

    console.log("Options after SSE (no manual refresh):", optionsAfterSSE);

    // Now trigger manual refresh
    await page.focus('#file-selector');
    await page.waitForTimeout(500); // Wait for manual refresh to complete

    const optionsAfterManual = await page.$$eval('#file-selector option', opts =>
        opts.map(opt => opt.value)
    );

    console.log("Options after manual refresh:", optionsAfterManual);

    // Compare the results
    const sseUpdatedCorrectly = optionsAfterSSE.length > initialOptions.length;
    const manualRefreshChanged = JSON.stringify(optionsAfterSSE) !== JSON.stringify(optionsAfterManual);

    return {
        initialCount: initialOptions.length,
        afterSSECount: optionsAfterSSE.length,
        afterManualCount: optionsAfterManual.length,
        sseWorking: sseUpdatedCorrectly,
        manualRefreshNeeded: manualRefreshChanged
    };
}

// Export test functions for use
if (typeof module !== 'undefined' && module.exports) {
    module.exports = {
        testDropdownSelectionDuringSSE,
        testRapidDropdownInteraction,
        testManualRefreshNecessity
    };
}

// Run tests if executed directly
async function runAllTests() {
    const { chromium } = require('playwright');
    const browser = await chromium.launch({ headless: false });
    const page = await browser.newPage();

    try {
        console.log("=== Running Dropdown SSE Edge Case Tests ===\n");

        const test1 = await testDropdownSelectionDuringSSE(page);
        console.log("Test 1 Results:", test1);

        const test2 = await testRapidDropdownInteraction(page);
        console.log("\nTest 2 Results:", test2);

        const test3 = await testManualRefreshNecessity(page);
        console.log("\nTest 3 Results:", test3);

        console.log("\n=== Analysis ===");
        console.log("Manual refresh needed?", test3.manualRefreshNeeded);
        console.log("SSE updates working?", test3.sseWorking);
        console.log("Dropdown focus preserved?", test1.dropdownFocusedAfterSSE);

    } catch (error) {
        console.error("Test failed:", error);
    } finally {
        await browser.close();
    }
}

// Uncomment to run tests directly
// runAllTests();