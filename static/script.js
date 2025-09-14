// Debounce utility function
function debounce(func, wait) {
    let timeout;
    return function executedFunction(...args) {
        const later = () => {
            clearTimeout(timeout);
            func(...args);
        };
        clearTimeout(timeout);
        timeout = setTimeout(later, wait);
    };
}

// State management for resize transitions
let resizeState = {
    scrollPositions: new Map(),
    isTransitioning: false,
    lastWidth: window.innerWidth
};

// Initialize chat state based on screen size
function initializeChatState() {
    const chat = document.getElementById('chat-container');
    if (!chat) return;
    
    if (window.innerWidth >= 1024) {
        // Desktop: Remove all mobile classes
        chat.classList.remove('chat-expanded', 'chat-minimized');
    } else {
        // Mobile: Set to expanded by default
        chat.classList.remove('chat-minimized');
        chat.classList.add('chat-expanded');
    }
}

// Handle Shift+Enter for newlines in message input
document.addEventListener('DOMContentLoaded', function() {
    // Single message input (works for both desktop and mobile)
    const messageInput = document.getElementById('message-input');
    if (messageInput) {
        // Auto-resize textarea as user types
        messageInput.addEventListener('input', function() {
            this.style.height = 'auto';
            this.style.height = Math.min(this.scrollHeight, 200) + 'px';
        });
    }
    
    // Scroll to bottom on page load if there are messages
    const messagesDiv = document.getElementById('messages');
    if (messagesDiv) {
        messagesDiv.scrollTop = messagesDiv.scrollHeight;
    }
    
    // Add outside click handler for mobile chat (only on screens < 1024px)
    document.addEventListener('click', function(event) {
        // Only apply outside click behavior on mobile screens
        if (window.innerWidth >= 1024) return;
        
        const chat = document.getElementById('chat-container');
        const messageInput = document.getElementById('message-input');
        const isClickInsideChat = chat && chat.contains(event.target);
        
        // Check if user is actively typing or has focus in the input
        const isTyping = messageInput && (
            document.activeElement === messageInput || 
            messageInput.value.trim().length > 0
        );
        
        // If chat is expanded, click is outside, and user is not typing, minimize it
        if (chat && !isClickInsideChat && chat.classList.contains('chat-expanded') && !isTyping) {
            chat.classList.remove('chat-expanded');
            chat.classList.add('chat-minimized');
        }
    });
    
    // Handle window resize for smooth desktop↔mobile transitions with debouncing
    const handleResize = debounce(function() {
        const chat = document.getElementById('chat-container');
        const messagesDiv = document.getElementById('messages');
        if (!chat) return;
        
        const currentWidth = window.innerWidth;
        const previousWidth = resizeState.lastWidth;
        
        // Detect if we're crossing the breakpoint
        const wasDesktop = previousWidth >= 1024;
        const isDesktop = currentWidth >= 1024;
        const crossingBreakpoint = wasDesktop !== isDesktop;
        
        if (crossingBreakpoint) {
            resizeState.isTransitioning = true;
            
            // Save scroll position before transition
            if (messagesDiv) {
                const scrollKey = wasDesktop ? 'desktop' : 'mobile';
                resizeState.scrollPositions.set(scrollKey, {
                    top: messagesDiv.scrollTop,
                    height: messagesDiv.scrollHeight,
                    isAtBottom: Math.abs(messagesDiv.scrollHeight - messagesDiv.scrollTop - messagesDiv.clientHeight) < 10
                });
            }
            
            // Apply new state
            if (isDesktop) {
                // Transitioning to desktop
                chat.classList.remove('chat-expanded', 'chat-minimized');
            } else {
                // Transitioning to mobile
                chat.classList.remove('chat-minimized');
                chat.classList.add('chat-expanded');
            }
            
            // Restore scroll position after transition
            setTimeout(() => {
                if (messagesDiv) {
                    const restoreKey = isDesktop ? 'desktop' : 'mobile';
                    const savedPosition = resizeState.scrollPositions.get(restoreKey);
                    
                    if (savedPosition) {
                        if (savedPosition.isAtBottom) {
                            // If was at bottom, stay at bottom
                            messagesDiv.scrollTop = messagesDiv.scrollHeight;
                        } else {
                            // Try to maintain relative position
                            const scrollRatio = savedPosition.top / savedPosition.height;
                            messagesDiv.scrollTop = messagesDiv.scrollHeight * scrollRatio;
                        }
                    } else {
                        // Default to bottom if no saved position
                        messagesDiv.scrollTop = messagesDiv.scrollHeight;
                    }
                }
                resizeState.isTransitioning = false;
            }, 350); // Slightly longer than CSS transition
        }
        
        resizeState.lastWidth = currentWidth;
    }, 200); // 200ms debounce delay
    
    window.addEventListener('resize', handleResize);
    
    // Initialize proper state on page load
    initializeChatState();
});

// Unified chat toggle function (mobile only) with debouncing
const toggleChat = debounce(function() {
    const chat = document.getElementById('chat-container');
    const messagesDiv = document.getElementById('messages');
    
    if (chat && !resizeState.isTransitioning) {
        const isMinimized = chat.classList.contains('chat-minimized');
        
        if (isMinimized) {
            chat.classList.remove('chat-minimized');
            chat.classList.add('chat-expanded');
            
            // Restore scroll position when expanding
            if (messagesDiv) {
                const savedPosition = resizeState.scrollPositions.get('mobile-minimized');
                if (savedPosition && savedPosition.isAtBottom) {
                    setTimeout(() => {
                        messagesDiv.scrollTop = messagesDiv.scrollHeight;
                    }, 300);
                }
            }
            
            // Focus on input when expanded (match transition duration)
            const messageInput = document.getElementById('message-input');
            if (messageInput) {
                setTimeout(() => messageInput.focus(), 300);
            }
        } else {
            // Save scroll position before minimizing
            if (messagesDiv) {
                resizeState.scrollPositions.set('mobile-minimized', {
                    top: messagesDiv.scrollTop,
                    height: messagesDiv.scrollHeight,
                    isAtBottom: Math.abs(messagesDiv.scrollHeight - messagesDiv.scrollTop - messagesDiv.clientHeight) < 10
                });
            }
            
            chat.classList.remove('chat-expanded');
            chat.classList.add('chat-minimized');
        }
    }
}, 100); // 100ms debounce for toggle

// Connection status flash management
function showFlash(message, type, autoHide) {
    const flash = document.getElementById('connection-flash');
    const messageEl = document.getElementById('flash-message');
    
    if (!flash || !messageEl) return;
    
    // Clear existing classes and state
    flash.classList.remove('hidden', 'bg-amber-500', 'bg-green-500', 'bg-red-500', 'flash-hidden', 'flash-visible');
    
    // Set message and show
    messageEl.textContent = message;
    flash.classList.add('flash-visible');
    
    // Apply color based on type
    switch(type) {
        case 'warning':
            flash.classList.add('bg-amber-500');
            break;
        case 'success':
            flash.classList.add('bg-green-500');
            break;
        case 'error':
            flash.classList.add('bg-red-500');
            break;
        default:
            flash.classList.add('bg-gray-500');
    }
    
    // Auto-hide if specified
    if (autoHide) {
        setTimeout(() => {
            flash.classList.remove('flash-visible');
            flash.classList.add('flash-hidden');
            setTimeout(() => flash.classList.add('hidden'), 300);
        }, autoHide);
    }
}

function hideFlash() {
    const flash = document.getElementById('connection-flash');
    if (!flash) return;
    
    flash.classList.remove('flash-visible');
    flash.classList.add('flash-hidden');
    setTimeout(() => flash.classList.add('hidden'), 300);
}

// SSE Connection Status Event Handlers
document.addEventListener('DOMContentLoaded', function() {
    // Listen for SSE connection events
    document.body.addEventListener('htmx:sseError', function(event) {
        console.log('SSE Error detected:', event.detail);
        showFlash('Connection lost - reconnecting...', 'warning');
    });
    
    document.body.addEventListener('htmx:sseOpen', function(event) {
        console.log('SSE Connection opened:', event.detail);
        showFlash('Connected', 'success', 2000); // Auto-hide after 2 seconds
    });
    
    document.body.addEventListener('htmx:sseClose', function(event) {
        console.log('SSE Connection closed:', event.detail);
        const reason = event.detail?.type || 'unknown';
        
        // Only show flash for unexpected closures, not intentional ones
        if (reason === 'nodeMissing' || reason === 'nodeReplaced') {
            // Don't show flash for these cases (likely page navigation)
            return;
        }
        
        showFlash('Connection closed', 'error');
    });
    
    // HTMX Send Error Event Handlers
    document.body.addEventListener('htmx:sendError', function(event) {
        console.log('Send Error detected:', event.detail);
        showFlash('Failed to send message - check your connection', 'error', 5000);
    });
    
    document.body.addEventListener('htmx:responseError', function(event) {
        console.log('Response Error detected:', event.detail);
        const status = event.detail.xhr?.status || 0;
        let message = 'Server error - please try again';
        
        if (status === 429) {
            message = 'Rate limited - please wait a moment';
        } else if (status >= 500) {
            message = 'Server temporarily unavailable';
        } else if (status === 401 || status === 403) {
            message = 'Session expired - please refresh the page';
        } else if (status >= 400) {
            message = 'Request failed - please check your input';
        }
        
        showFlash(message, 'error', 8000);
    });
    
    document.body.addEventListener('htmx:timeout', function(event) {
        console.log('Request timeout detected:', event.detail);
        showFlash('Request timed out - please try again', 'warning', 5000);
    });

    // Auto-scroll on SSE messages
    document.body.addEventListener('htmx:sseMessage', function(event) {
        const messagesDiv = document.getElementById('messages');
        if (!messagesDiv) return;

        // Check if user is near bottom (within 100px threshold)
        const isNearBottom = messagesDiv.scrollHeight - messagesDiv.scrollTop - messagesDiv.clientHeight < 100;

        // Only auto-scroll if user was already near bottom
        if (isNearBottom) {
            requestAnimationFrame(() => {
                messagesDiv.scrollTop = messagesDiv.scrollHeight;
            });
        }
    });

});