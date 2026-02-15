# Differentiation Brief: OpenCode Chat vs exe.dev

## Situation

exe.dev ships the same core thesis: persistent VM + AI agent + instant HTTPS.
They're ahead on infrastructure. We differentiate on **operational UX**.

## exe.dev's Friction Points (observed firsthand)

1. **Multi-surface workflow**: Dashboard to create VM, browser tab for Shelley,
   separate shell session to run `ssh exe.dev share set-public <vm>`, back to
   browser to verify. Minimum 3 tabs + 1 terminal.
2. **Shell-gated operations**: Making a site public, changing proxy ports,
   sharing with users — all require SSH commands.
3. **No credential management**: Users paste API keys into the VM themselves.
   Keys sit in env vars or dotfiles with no rotation, no auditing, no UI.
4. **Mobile gap**: Shelley works on mobile. Everything else (VM creation,
   sharing, port config) requires SSH, which is hostile on phones.

## Our Three Concrete Differentiators

### 1. Single-Page Operations (no tab-switching, no shell)

Everything exe.dev does across dashboard + shell + Shelley, we do in one page.

**What to build:**
- [ ] One-click "Make Public" toggle in the preview tab header. Calls Fly.io
      Machines API to update firewall rules. No shell, no SSH.
- [ ] "Share" button that generates a public URL and copies to clipboard.
- [ ] Port selector dropdown in preview tab (exe.dev: `ssh exe.dev share port <vm> <port>`).
- [ ] Custom domain config in a settings modal (exe.dev: DNS + CNAME docs).

**Implementation**: These are HTTP handlers that call the Fly.io Machines API
or manage nginx/caddy config on the VM. The UI is HTMX partials — swap a
toggle, return the new state.

### 2. Mobile-First Throughout

Shelley is mobile-friendly. The rest of exe.dev is not. We make every
operation work on a phone.

**What to build:**
- [ ] VM creation from mobile (tap "New Project", no SSH).
- [ ] The Make Public toggle, share button, port selector, model picker —
      all must work at 375px width with 44px touch targets.
- [ ] Responsive preview tab: full-width iframe on mobile with overlay controls.

**Implementation**: Already have responsive chat + preview. Extend to all
operational controls. Bottom sheet modals for settings on mobile.

### 3. Credential Proxy

exe.dev has zero credential management. Users paste `ANTHROPIC_API_KEY` into
their VM shell. This is our biggest feature gap to exploit.

**What to build:**
- [ ] Server-side credential vault: user enters API keys once in a settings
      page. Keys stored encrypted, never sent to the browser, injected into
      the sandbox as env vars at boot.
- [ ] Provider picker in chat UI: select OpenAI/Anthropic/Google, key is
      resolved server-side. User never sees the key in the VM.
- [ ] Key rotation UI: revoke/replace keys without SSH.
- [ ] Usage dashboard: show token counts and costs per session (read from
      provider APIs or parse sandbox logs).

**Implementation**: Keys stored in server-side session or encrypted DB.
Injected into Fly.io machine env vars via Machines API on create. The
sandbox never exposes keys to the terminal or filesystem — they exist only
as env vars in the OpenCode process, not in dotfiles.

## Honest Assessment: Does Single-Page Matter?

On desktop, the single-page vs multi-tab distinction is a minor convenience.
Power users don't care about an extra tab.

On mobile, it matters a lot. You can't easily juggle 3 browser tabs + an SSH
app on a phone. A single-page app with all controls inline is categorically
better on mobile.

The real value isn't "single page" — it's **zero-shell operations**. Every
action exe.dev gates behind `ssh exe.dev <command>`, we expose as a button.
That's the gap.

## Priority Order

1. **Make Public toggle** — smallest lift, most visible differentiator.
   Requires Fly.io sandbox to be implemented (`sandbox_flyio.go`).
2. **Credential proxy** — biggest moat. exe.dev can't easily retrofit this
   because their architecture gives users raw VM access (keys in dotfiles).
   Our architecture controls the sandbox entry point, so we can inject keys
   without exposing them.
3. **Mobile operations** — extend existing responsive layout to cover all
   new controls.

## Non-Goals

- Don't compete on infrastructure (VM count, CPU/RAM, disk). Use Fly.io.
- Don't build SSH access. That's exe.dev's lane.
- Don't build team/SSO features yet. Individual users first.
