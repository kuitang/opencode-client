// Enable HTMX logging for debugging
htmx.logAll();
console.log("HTMX logging enabled");

// Handle Shift+Enter for newlines in message input
document.addEventListener('DOMContentLoaded', function() {
    const messageInput = document.getElementById('message-input');
    const messageForm = document.getElementById('message-form');
    
    if (messageInput) {
        // Auto-resize textarea as user types
        messageInput.addEventListener('input', function() {
            this.style.height = 'auto';
            this.style.height = Math.min(this.scrollHeight, 200) + 'px';
        });
        
        // Handle Enter vs Shift+Enter
        messageInput.addEventListener('keydown', function(e) {
            if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault();
                if (this.value.trim()) {
                    messageForm.requestSubmit();
                }
            }
            // Shift+Enter will naturally add a newline
        });
    }
});

// Handle SSE messages with OOB support
document.body.addEventListener('htmx:sseMessage', function(evt) {
    // Parse the incoming HTML to check for OOB elements
    const parser = new DOMParser();
    const doc = parser.parseFromString(evt.detail.data, 'text/html');
    const element = doc.body.firstElementChild;
    
    if (element && element.hasAttribute('hx-swap-oob')) {
        // This is an OOB update
        evt.preventDefault(); // Prevent default SSE handling
        
        const targetId = element.id;
        if (targetId) {
            const target = document.getElementById(targetId);
            if (target) {
                // Replace the existing element
                target.outerHTML = evt.detail.data;
                // Process htmx attributes on the new element
                htmx.process(document.getElementById(targetId));
            }
        }
    }
    // If not OOB, let htmx handle it normally
});