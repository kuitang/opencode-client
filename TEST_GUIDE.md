# Test Guide for OpenCode Client

## Overview
This repository contains multiple JavaScript test files. They fall into two categories:

### 1. Playwright Node.js Tests (Require Installation)
These tests launch a real browser and interact with the application.

### 2. Browser-Executable Tests
These can be run directly in the browser console via `page.evaluate()`.

## Test Files and Their Purpose

| File | Type | Purpose | How to Run |
|------|------|---------|------------|
| `playwright-session-helper.js` | Helper | Session management utilities | Imported by other tests |
| `playwright_terminal_test.js` | Playwright | Tests terminal/gotty integration | `node playwright_terminal_test.js` |
| `test_preview_integration.js` | Playwright | Full preview feature integration test | `node test_preview_integration.js` |
| `test_preview_playwright.js` | Browser/Module | Preview tab, Kill button tests | Via browser or import |
| `test_dropdown_sse_edge_case.js` | Playwright | SSE dropdown edge cases | `node test_dropdown_sse_edge_case.js` |
| `test_flush_alignment.js` | Browser | Connection flash alignment | Via browser console |
| `test_resize_scenarios.js` | Browser | UI resize/scroll tests | Via browser console |
| `test_sse.js` | Browser | SSE message streaming | Via browser console |
| `test_sse_oob_updates.js` | Browser | Out-of-band HTMX updates | Via browser console |
| `test_sse_scroll_playwright.js` | Playwright | SSE with scroll behavior | `node test_sse_scroll_playwright.js` |
| `run_preview_tests.js` | Playwright | Runs preview feature tests | `node run_preview_tests.js` |

## Installation Requirements

### For Playwright Tests
```bash
# First time setup
npm init -y
npm install playwright

# Install browsers (one time)
npx playwright install chromium
```

### For Browser Console Tests
No installation needed - run directly in browser DevTools.

## Running the Tests

### Method 1: Run Playwright Tests (After Installation)
```bash
# Make sure server is running
./opencode-chat -port 8085 &

# Run individual test
node test_preview_integration.js

# Or run all Playwright tests
for test in playwright_terminal_test.js test_preview_integration.js test_dropdown_sse_edge_case.js test_sse_scroll_playwright.js run_preview_tests.js; do
    echo "Running $test..."
    node $test
done
```

### Method 2: Run Browser Console Tests (No Installation)
1. Open browser and navigate to `http://localhost:8085`
2. Open DevTools Console (F12)
3. Copy and paste the test file contents
4. Call the test functions:
```javascript
// Example for test_sse.js
testSSEConnection();
testMessageStreaming();

// Example for test_resize_scenarios.js
testScrollPositionPreservation();
testRapidResizeTransitions();
```

### Method 3: Run via Playwright's page.evaluate() (Hybrid)
```javascript
// In a Playwright script
const testCode = fs.readFileSync('test_preview_playwright.js', 'utf8');
await page.evaluate(testCode);
await page.evaluate(() => testFirstLoadShowsPreview());
```

## Quick Test Without Playwright

If you don't want to install Playwright, you can test core functionality with curl:

```bash
# Test 1: First load shows preview content
curl -s http://localhost:8085/ | grep -c "No Application Running"

# Test 2: Preview tab endpoint
curl -s http://localhost:8085/tab/preview | grep -c "preview-iframe\|No Application Running"

# Test 3: Kill endpoint exists
curl -X POST http://localhost:8085/kill-preview-port -d "port=9999" -v 2>&1 | grep "< HTTP"

# Test 4: Test with a real server
python3 -m http.server 5555 &
sleep 2
curl -s http://localhost:8085/tab/preview | grep -c "Port"
kill %1  # Kill the Python server
```

## Manual Browser Testing

1. **Test First Load**:
   - Open `http://localhost:8085`
   - Should see "No Application Running" in preview area

2. **Test Kill Button**:
   - Start a server: Ask the assistant to create a Python HTTP server
   - Go to Preview tab
   - Click Kill button
   - Should return to "No Application Running"

3. **Test SSE Streaming**:
   - Send a message in chat
   - Watch for smooth streaming of response

4. **Test Resize Behavior**:
   - Resize browser window
   - Check that scroll position is preserved
   - Toggle mobile/desktop view

## Common Issues

### "Cannot find module 'playwright'"
- Solution: Run `npm install playwright` first

### "Server is not running on port 8085"
- Solution: Start server with `./opencode-chat -port 8085`

### "Connection refused" errors
- Solution: Check Docker is running for sandbox
- Solution: Restart the server

### Tests timeout
- Solution: Increase timeout values in test files
- Solution: Check server logs in `/tmp/opencode.log`

## Test Development

To add new tests:

1. **For Playwright tests**: Follow pattern in `test_preview_integration.js`
2. **For browser tests**: Follow pattern in `test_preview_playwright.js`
3. **Naming convention**: `test_<feature>_<type>.js`
4. **Always include**: Error handling, screenshots on failure, clear console output

## CI/CD Integration

For GitHub Actions or other CI:

```yaml
- name: Install dependencies
  run: |
    npm install playwright
    npx playwright install chromium

- name: Start server
  run: ./opencode-chat -port 8085 &

- name: Wait for server
  run: sleep 10

- name: Run tests
  run: |
    node test_preview_integration.js
    node test_dropdown_sse_edge_case.js
```