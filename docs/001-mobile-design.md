# VibeCoding Mobile Design System

## Overview

VibeCoding uses a **unified responsive interface** that eliminates duplication by having a single chat and content system that adapts via CSS and JavaScript across desktop and mobile layouts.

## Core Design Principles

### 1. Single Source of Truth
- **One Chat Interface**: `#chat-container` works for both desktop sidebar and mobile modal
- **One Content Area**: `#main-content` serves all tab content across layouts  
- **One SSE Connection**: Unified streaming prevents state sync issues
- **No Duplication**: Changes apply universally, reducing maintenance burden

### 2. Responsive Layout Strategy

**Desktop (≥1024px)**:
- Chat: Fixed left sidebar (1/3 width)
- Main: Tab content area (2/3 width) 
- Behavior: Always visible, no toggle needed

**Mobile (<1024px)**:
- Chat: Bottom modal overlay (80vh when expanded)
- Main: Full-width content with tab navigation
- Behavior: Starts expanded, can minimize to header-only

## Implementation Architecture

### HTML Structure
```html
<!-- Single responsive layout -->
<div class="flex h-full lg:static relative">
  <!-- Unified chat container -->
  <aside id="chat-container" class="chat-responsive">
    <header>Chat + Toggle</header>
    <div id="messages">SSE messages</div>
    <form id="message-form">Input + Provider/Model</form>
  </aside>
  
  <!-- Unified main content -->
  <main>
    <nav>Tab buttons → hx-target="#main-content"</nav>
    <section id="main-content">Tab content</section>
  </main>
</div>
```

### CSS Responsive Classes
```css
/* Desktop: Static sidebar behavior */
@media (min-width: 1024px) {
  .chat-responsive { position: static; width: 33.333%; }
}

/* Mobile: Modal behavior with states */
@media (max-width: 1023px) {
  .chat-responsive { position: fixed; bottom: 0; }
  .chat-minimized { height: 64px; overflow: hidden; }
  .chat-expanded { height: 80vh; display: flex; flex-direction: column; }
}
```

### JavaScript State Management
```javascript
// Smooth desktop↔mobile transitions
window.addEventListener('resize', function() {
  if (width >= 1024) {
    // Desktop: Remove mobile classes
  } else {
    // Mobile: Set to expanded state
  }
});
```

## Mobile UX Patterns

### Chat Modal States
- **Minimized**: Header bar only (64px height), up-arrow chevron (⬆️)
- **Expanded**: 80vh modal with down-arrow chevron (⬇️), sticky form at bottom
- **Transitions**: 300ms CSS transforms for smooth expand/minimize

### Touch Target Compliance
- **Minimum Size**: 44px (iOS requirement) using `w-11 h-11` or `min-h-[44px]`
- **Interactive Elements**: Buttons, form inputs, tap areas all meet standards
- **Accessibility**: Proper ARIA labels and semantic HTML structure

### Provider/Model Accessibility
- **Always Visible**: Sticky positioning prevents scroll-off issues
- **Preserved Layout**: `visibility: hidden/visible` maintains flex structure
- **Mobile Optimized**: Smaller text (`text-xs`) but same functionality

## HTMX Integration

### Tab System
- **Unified Targets**: All tabs use `hx-target="#main-content"`
- **Active States**: `htmx.takeClass(this, 'active')` for visual feedback
- **Server Routes**: `/tab/preview`, `/tab/code`, `/tab/deployment`

### SSE Streaming
- **Single Connection**: `hx-ext="sse" sse-connect="/events"`
- **Message Targeting**: `sse-swap="message" hx-swap="beforeend scroll:bottom"`
- **Continuous Scroll**: No page refreshes, smooth streaming experience

## Contributing Guidelines

### Adding New Features

**New Tab Content**:
1. Create template in `templates/tabs/newtab.html`
2. Add route handler in `main.go`: `handleTabNewTab()`
3. Add tab button targeting `#main-content`
4. Follow responsive spacing: `lg:p-6 max-lg:p-4`

**Chat Enhancements**:
- Modify single `#chat-container` - changes apply to all layouts
- Test both desktop sidebar and mobile modal states
- Ensure provider/model selectors remain accessible

**Mobile-Specific Features**:
- Use `lg:hidden` or `max-lg:` classes for mobile-only elements
- Test minimize/maximize cycles for state persistence
- Verify 44px touch target compliance

### Design Language Consistency

**Colors**:
- Chat header: `bg-gray-800` (dark)
- Content areas: `bg-gray-50` (light)
- Borders: `border-gray-200` (subtle)
- Primary actions: `bg-blue-600` (brand)

**Typography**:
- Headers: `text-lg font-semibold` (desktop), `text-xl` (mobile)
- Body text: `text-gray-600` for secondary, `text-gray-900` for primary
- Form labels: `text-gray-600` with responsive sizing

**Spacing**:
- Desktop: `p-6` for generous spacing
- Mobile: `p-4` for efficient space usage
- Touch targets: `w-11 h-11` minimum (44px)

### Testing Requirements

**Responsive Testing**:
1. Start desktop, send message, resize to mobile
2. Verify messages preserved, providers visible, chevron correct
3. Test minimize/maximize cycles
4. Send mobile message, verify SSE streaming

**Cross-Device Validation**:
- Desktop: 1280x720+ for sidebar layout
- Tablet: 768-1023px for transitional behavior  
- Mobile: 375x812 for modal layout
- Touch targets: Verify 44px minimum on all devices

## Architecture Benefits

- **🔄 Zero Duplication**: Single codebase, no sync issues
- **⚡ Performance**: One SSE connection, efficient DOM
- **🛠️ Maintainable**: Changes apply universally
- **📱 Mobile-First**: Optimized for touch interaction
- **🎨 Consistent**: Unified design language across breakpoints

This system ensures new features integrate seamlessly while maintaining the responsive, unified experience across all device types.