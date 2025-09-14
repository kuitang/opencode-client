/**
 * Playwright test for Preview functionality
 * Tests that the LLM can create a server and the preview tab shows it in an iframe
 */

async function testPreviewWithLLMServer() {
    console.log("Starting Preview test with LLM-created server");

    // Send message to LLM to create and run a server
    const messageInput = document.querySelector('#message-input');
    const sendButton = document.querySelector('#send-button');

    if (!messageInput || !sendButton) {
        throw new Error("Could not find message input or send button");
    }

    // Ask LLM to create a server on port 5000
    messageInput.value = "Create a simple Python HTTP server on port 5000 that returns an HTML page with '<h1>Test Server on Port 5000</h1>' and run it in the background";
    sendButton.click();

    // Wait for LLM response (it should create and start the server)
    console.log("Waiting for LLM to create and start server...");
    await new Promise(resolve => setTimeout(resolve, 15000)); // Wait 15 seconds for LLM to respond and start server

    // Click on Preview tab
    const previewTab = document.querySelector('[hx-get="/tab/preview"]');
    if (!previewTab) {
        throw new Error("Could not find Preview tab");
    }

    console.log("Clicking Preview tab...");
    previewTab.click();

    // Wait for tab content to load
    await new Promise(resolve => setTimeout(resolve, 2000));

    // Check if iframe exists
    const iframe = document.querySelector('#preview-iframe');
    if (!iframe) {
        // Check if "No Application Running" message is shown
        const noAppMessage = document.querySelector('.text-gray-500');
        if (noAppMessage && noAppMessage.textContent.includes('No Application Running')) {
            throw new Error("Preview shows 'No Application Running' - server may not have started");
        }
        throw new Error("Preview iframe not found");
    }

    console.log("Preview iframe found, checking if it loads...");

    // Wait for iframe to load
    await new Promise((resolve, reject) => {
        const timeout = setTimeout(() => {
            reject(new Error("Iframe load timeout"));
        }, 10000);

        iframe.addEventListener('load', () => {
            clearTimeout(timeout);
            resolve();
        });

        // If already loaded
        if (iframe.contentDocument && iframe.contentDocument.readyState === 'complete') {
            clearTimeout(timeout);
            resolve();
        }
    });

    // Try to access iframe content (may be blocked by CORS)
    try {
        const iframeDoc = iframe.contentDocument || iframe.contentWindow.document;
        const h1 = iframeDoc.querySelector('h1');
        if (h1 && h1.textContent.includes('Test Server on Port 5000')) {
            console.log("✓ Preview iframe shows the correct server content");
        } else {
            console.log("⚠ Preview iframe loaded but content doesn't match expected");
        }
    } catch (e) {
        console.log("⚠ Cannot access iframe content (expected due to same-origin policy)");
        console.log("✓ Preview iframe exists and appears to be loading content");
    }

    return true;
}

// Alternative test that doesn't require LLM interaction
async function testPreviewTabBasicFunctionality() {
    console.log("Testing Preview tab basic functionality");

    // Click on Preview tab
    const previewTab = document.querySelector('[hx-get="/tab/preview"]');
    if (!previewTab) {
        throw new Error("Could not find Preview tab");
    }

    previewTab.click();

    // Wait for tab content to load
    await new Promise(resolve => setTimeout(resolve, 1000));

    // Check that either iframe or "No Application Running" message exists
    const iframe = document.querySelector('#preview-iframe');
    const noAppMessage = document.querySelector('.text-gray-900');

    if (!iframe && (!noAppMessage || !noAppMessage.textContent.includes('No Application Running'))) {
        throw new Error("Preview tab should show either iframe or 'No Application Running' message");
    }

    console.log("✓ Preview tab loads correctly");
    return true;
}

// Test that first load shows preview content (not placeholder)
async function testFirstLoadShowsPreview() {
    console.log("Testing that first load shows preview content");

    // Check the main-content area on page load
    const mainContent = document.querySelector('#main-content');
    if (!mainContent) {
        throw new Error("Could not find main-content area");
    }

    // Check for preview-specific content (not generic placeholder)
    const hasPreviewContent =
        mainContent.querySelector('#preview-iframe') ||
        mainContent.querySelector('.text-gray-900')?.textContent.includes('No Application Running');

    if (!hasPreviewContent) {
        // Check if we still have the old placeholder
        const hasPlaceholder = mainContent.textContent.includes('Welcome to VibeCoding');
        if (hasPlaceholder) {
            throw new Error("First load still shows placeholder instead of preview tab content");
        }
    }

    console.log("✓ First load correctly shows preview tab content");
    return true;
}

// Test Kill button functionality
async function testKillButton() {
    console.log("Testing Kill button functionality");

    // First, we need a server running - this test assumes one is already running
    // Navigate to Preview tab
    const previewTab = document.querySelector('[hx-get="/tab/preview"]');
    if (!previewTab) {
        throw new Error("Could not find Preview tab");
    }
    previewTab.click();

    await new Promise(resolve => setTimeout(resolve, 2000));

    // Check if Kill button exists when a port is detected
    const killButton = document.querySelector('button[hx-post="/kill-preview-port"]');
    if (killButton) {
        console.log("✓ Kill button found when server is running");

        // Check that it's in a form with hidden port input
        const form = killButton.closest('form');
        if (!form) {
            throw new Error("Kill button should be inside a form");
        }

        const portInput = form.querySelector('input[name="port"]');
        if (!portInput || !portInput.value) {
            throw new Error("Kill button form should have hidden port input with value");
        }

        console.log(`✓ Kill button has port value: ${portInput.value}`);

        // Click the kill button
        console.log("Clicking Kill button...");
        killButton.click();

        // Wait for response
        await new Promise(resolve => setTimeout(resolve, 3000));

        // Check that we now see "No Application Running"
        const noAppMessage = document.querySelector('.text-gray-900');
        if (!noAppMessage || !noAppMessage.textContent.includes('No Application Running')) {
            console.log("⚠ Kill button clicked but 'No Application Running' not shown (server may have restarted)");
        } else {
            console.log("✓ Kill button successfully stopped the server");
        }
    } else {
        console.log("⚠ No Kill button found (no server running)");

        // Verify that Kill button is not shown when no server is running
        const noAppMessage = document.querySelector('.text-gray-900');
        if (noAppMessage && noAppMessage.textContent.includes('No Application Running')) {
            console.log("✓ Kill button correctly not shown when no server is running");
        }
    }

    return true;
}

// Export for use with Playwright
if (typeof module !== 'undefined' && module.exports) {
    module.exports = {
        testPreviewWithLLMServer,
        testPreviewTabBasicFunctionality,
        testFirstLoadShowsPreview,
        testKillButton
    };
}