// Helper script to work around Playwright's HttpOnly cookie limitation
// This script intercepts session creation and stores it in localStorage

(function() {
    // Store original fetch
    const originalFetch = window.fetch;
    
    // Override fetch to capture session cookie from responses
    window.fetch = function(...args) {
        return originalFetch.apply(this, args).then(response => {
            // Check if this is a send request that might create a session
            const url = args[0];
            if (url === '/send' || url === '/') {
                // Try to extract session from response headers (won't work for HttpOnly)
                // Instead, we'll store when we see successful responses
                if (response.ok) {
                    // Mark that we have an active session
                    localStorage.setItem('hasActiveSession', 'true');
                    console.log('[Session Helper] Marked session as active');
                }
            }
            return response;
        });
    };
    
    // On page load, if we have a stored session marker, try to restore functionality
    if (localStorage.getItem('hasActiveSession') === 'true') {
        console.log('[Session Helper] Found stored session marker');
        
        // Inject a non-HttpOnly cookie that our script can read
        // This won't affect server-side session handling but will trigger our client-side fetch
        document.cookie = 'session=playwright_restored; path=/';
        console.log('[Session Helper] Injected marker cookie');
    }
    
    console.log('[Session Helper] Initialized');
})();