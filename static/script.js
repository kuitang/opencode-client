// Handle Shift+Enter for newlines in message input
document.addEventListener('DOMContentLoaded', function() {
    const messageInput = document.getElementById('message-input');
    
    if (messageInput) {
        // Auto-resize textarea as user types
        messageInput.addEventListener('input', function() {
            this.style.height = 'auto';
            this.style.height = Math.min(this.scrollHeight, 200) + 'px';
        });
        
        // Enter key handling done via HTMX hx-trigger
        // Shift+Enter naturally adds newlines
    }
    
    // Scroll to bottom on page load if there are messages
    const messagesDiv = document.getElementById('messages');
    if (messagesDiv) {
        messagesDiv.scrollTop = messagesDiv.scrollHeight;
    }
});