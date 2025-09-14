# JavaScript Test File Types

## Files Requiring Node.js + Playwright (4 files)

These MUST be run with Node.js after installing Playwright:

1. **`test_preview_integration.js`**
   - Full integration test with browser automation
   - Requires: `const { chromium } = require('playwright');`
   - Run: `node test_preview_integration.js`

2. **`run_preview_tests.js`**
   - Comprehensive preview feature tests
   - Requires: `const { chromium } = require('playwright');`
   - Run: `node run_preview_tests.js`

3. **`test_dropdown_sse_edge_case.js`**
   - Has a `runAllTests()` function at bottom that requires Playwright
   - Mixed: Browser functions + Node.js runner
   - Run: `node test_dropdown_sse_edge_case.js` (for full test)
   - OR: Copy individual functions to browser console

4. **`test_sse_oob_updates.js`**
   - Contains Node.js requires (`child_process`, `util`) inside functions
   - Mixed: Some functions need Node.js, others are browser-compatible
   - Run: `node test_sse_oob_updates.js` (if it has a runner)
   - OR: Copy browser-compatible functions to console

## Browser-Only Test Files (7 files)

These can be run directly in the browser console OR via Playwright's `page.evaluate()`:

1. **`playwright-session-helper.js`**
   - Session helper utilities
   - Usage: Import/paste into browser console

2. **`playwright_terminal_test.js`**
   - Terminal/Gotty tests
   - Usage: Paste in console, call functions

3. **`test_preview_playwright.js`**
   - Preview tab and Kill button tests
   - Usage: Paste in console, call `testFirstLoadShowsPreview()`, etc.
   - Has `module.exports` for optional Node.js import

4. **`test_flush_alignment.js`**
   - Connection flash UI tests
   - Usage: Paste in console, call test functions

5. **`test_resize_scenarios.js`**
   - UI resize and scroll preservation tests
   - Usage: Paste in console, call `testScrollPositionPreservation()`, etc.

6. **`test_sse.js`**
   - SSE connection and streaming tests
   - Usage: Paste in console, call `testSSEConnection()`, etc.

7. **`test_sse_scroll_playwright.js`**
   - SSE with scroll behavior tests
   - Usage: Paste in console, call test functions

## How to Run Each Type

### For Node.js/Playwright Tests:
```bash
# One-time setup
npm init -y
npm install playwright
npx playwright install chromium

# Run tests
node test_preview_integration.js
node run_preview_tests.js
node test_dropdown_sse_edge_case.js
```

### For Browser-Only Tests:
```javascript
// Method 1: Direct in browser console
// 1. Open http://localhost:8085 in browser
// 2. Open DevTools Console (F12)
// 3. Copy entire test file contents
// 4. Paste into console
// 5. Call functions:

testFirstLoadShowsPreview();  // From test_preview_playwright.js
testScrollPositionPreservation();  // From test_resize_scenarios.js
testSSEConnection();  // From test_sse.js

// Method 2: Via Playwright (if installed)
const { chromium } = require('playwright');
const fs = require('fs');

(async () => {
    const browser = await chromium.launch();
    const page = await browser.newPage();
    await page.goto('http://localhost:8085');

    // Load and execute browser test
    const testCode = fs.readFileSync('test_preview_playwright.js', 'utf8');
    await page.evaluate(testCode);

    // Run specific test
    const result = await page.evaluate(() => testFirstLoadShowsPreview());
    console.log('Test result:', result);

    await browser.close();
})();
```

## Quick Reference

| File | Type | Requires Installation | How to Run |
|------|------|----------------------|------------|
| `test_preview_integration.js` | Node.js | Yes (Playwright) | `node <file>` |
| `run_preview_tests.js` | Node.js | Yes (Playwright) | `node <file>` |
| `test_dropdown_sse_edge_case.js` | Mixed | Yes for full test | `node <file>` or browser |
| `test_sse_oob_updates.js` | Mixed | Yes for some functions | `node <file>` or browser |
| All other test files | Browser | No | Browser console |

## Testing Without Any Installation

If you don't want to install anything:
1. Use the provided `run_tests_no_playwright.sh` script for basic testing
2. Copy browser-only test files into DevTools console
3. Run individual test functions manually

## Which Approach to Use?

- **For CI/CD**: Use Node.js/Playwright tests (automated, headless)
- **For quick debugging**: Use browser console tests (immediate, visual)
- **For comprehensive testing**: Run both types
- **For no installation**: Use browser console or shell script