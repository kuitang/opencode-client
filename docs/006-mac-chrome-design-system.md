# 006: Mac Chrome Design System

## Overview

This document describes the Mac OS window chrome design system implemented across all tabs (Preview, Code, Terminal, Deployment) to provide a consistent, professional Mac application appearance.

## Design Goals

- **Visual Consistency**: All tabs use identical Mac OS window chrome
- **Professional Appearance**: Traffic light buttons and proper window styling
- **Maintainability**: Single pattern for future tabs
- **Responsive Design**: Works across mobile and desktop

## Chrome Structure

All tabs now use this **exact same structure**:

```html
<!-- Tab Container with Mac Chrome -->
<div class="bg-white rounded-lg shadow-lg border border-gray-200">
    <!-- Mac OS Chrome Toolbar - IDENTICAL across all tabs -->
    <div class="flex items-center justify-between px-6 py-4 border-b border-gray-200 bg-gray-50 rounded-t-lg min-h-[80px]">
        <div class="flex items-center space-x-4">
            <!-- Traffic lights FIRST -->
            <div class="flex items-center space-x-2">
                <div class="w-3 h-3 bg-red-500 rounded-full"></div>
                <div class="w-3 h-3 bg-yellow-500 rounded-full"></div>
                <div class="w-3 h-3 bg-green-500 rounded-full"></div>
            </div>
            <!-- Title AFTER traffic lights -->
            <h3 class="text-lg font-semibold text-gray-700">[TAB TITLE]</h3>
            <!-- Optional left content -->
            [TAB-SPECIFIC LEFT CONTENT]
        </div>
        <!-- Optional right content -->
        [TAB-SPECIFIC RIGHT CONTENT]
    </div>
    <!-- Content area -->
    <div class="p-0">
        [TAB-SPECIFIC MAIN CONTENT]
    </div>
</div>
```

## Critical CSS Classes

These classes **MUST** be identical across all tabs:

- **Container**: `bg-white rounded-lg shadow-lg border border-gray-200`
- **Toolbar**: `flex items-center justify-between px-6 py-4 border-b border-gray-200 bg-gray-50 rounded-t-lg min-h-[80px]`
- **Traffic lights**: `w-3 h-3 bg-red-500 rounded-full` (red), `bg-yellow-500` (yellow), `bg-green-500` (green)
- **Title**: `text-lg font-semibold text-gray-700`
- **Content area**: `p-0`

## Height Consistency

The `min-h-[80px]` on the toolbar ensures all tabs have identical chrome height regardless of content:
- Accommodates `min-h-[44px]` mobile-friendly buttons
- Provides consistent baseline for tabs with different toolbar content
- Uses flexbox `items-center` for vertical centering

## Implementation Approach

**Copy-Paste over Template Abstraction**: While Go templates support shared components via `dict` functions, we chose direct copy-paste because:

1. **Each tab has unique toolbar content**:
   - Preview: Port status + Refresh/Open buttons
   - Code: File selector + Copy/Download buttons
   - Terminal: Connection status
   - Deployment: No right content

2. **Simpler maintenance**: Direct HTML is easier to debug than nested template functions
3. **Performance**: No template function overhead
4. **Clear visual hierarchy**: Easy to see exactly what each tab contains

## Adding New Tabs

When adding new tabs, follow this pattern:

1. **Copy the exact chrome structure** from any existing tab
2. **Update the title** in the `<h3>` element
3. **Customize left/right content** as needed for tab functionality
4. **Keep all CSS classes identical** for the chrome container and toolbar
5. **Test visual consistency** with existing tabs

## Terminal Scrollbar Solution

The terminal scrollbar was hidden using Gotty's native configuration:

**File**: `sandbox/.gotty`
```
preferences {
    scrollbar_visible = false
}
```

**Implementation**:
- Added config file to Docker image via `COPY sandbox/.gotty /app/.gotty`
- Updated entrypoint.sh to use `--config /app/.gotty`
- This is **much better** than CSS hacks because it uses Gotty's built-in feature

## Rejected Approaches

1. **Shared Template Component**: Too complex for 4 tabs with different content
2. **CSS Injection**: Fragile and didn't work due to iframe isolation
3. **External CSS Targeting**: Couldn't reach iframe content from parent

## Future Considerations

- If tabs become more numerous (10+), consider shared template component
- For complex chrome variations, use template partials
- Always prioritize native configuration over CSS workarounds
- Test visual consistency across all tabs when making changes