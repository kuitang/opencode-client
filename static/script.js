// Enable HTMX logging for debugging
htmx.logAll();
console.log("HTMX logging enabled");

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