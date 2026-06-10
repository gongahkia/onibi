# DESIGN.md — Ghostty design language reference

A condensed reference describing **Ghostty's visual and interaction design**, intended to be applied to Onibi (Tauri+React desktop, mobile PWA). Sourced from `ghostty.org`, the Ghostty config reference, Mitchell Hashimoto's writing, and community config blogs (June 2026).

This document deliberately captures what Ghostty *does* and *does not* do. It is not a rewrite of Ghostty docs — it is the **design distillation** that another product can adopt.

---

## 1. Core philosophy

> "Speed, features, native UI — all three, no trade-offs."

Three non-negotiables Ghostty resolves simultaneously:

1. **Fast** — GPU-rendered (Metal on macOS, OpenGL on Linux), measured in microseconds-per-frame; rendering never blocks input.
2. **Feature-rich** — Kitty graphics, Kitty keyboard, sixel, hyperlinks, ligatures, light/dark notifications, etc. The product does not apologize for breadth.
3. **Platform-native** — AppKit/Swift on macOS, GTK4/Zig on Linux. **Not** an Electron-style cross-platform shell with a single UI. Each platform's UI matches its OS conventions.

**Architectural consequence:** `libghostty` (C-ABI) separates terminal emulation from UI. The UI layer is rewritten per platform; the emulator is shared. *Implication for Onibi*: the UX layer can be expressed in platform-native idioms even when the core (Rust/Tauri) is cross-platform — what matters is that the *result feels native*, not that the code is.

**Anti-philosophy** (what Ghostty deliberately is NOT):
- Not an IDE. Not a "developer cockpit." Not a multiplexer. Not a tab manager. Not a configuration UI.
- No custom-drawn tabs, splits, or dialogs (when a native equivalent exists).
- No telemetry, no accounts, no cloud sync.
- No tutorial / onboarding wizard — sensible defaults instead.

---

## 2. Visual restraint

The single most defining property of Ghostty's look is **how much is missing**.

### What you see at rest

- A window. Native title bar with platform traffic lights / window controls.
- A terminal cell grid filling the window.
- Optionally a native tab bar (only when ≥2 tabs exist — `window-show-tab-bar = auto`).
- That's it.

### What you do NOT see

- No sidebar.
- No status bar (no battery, no path, no git branch, no FPS counter).
- No icon bar / toolbar / ribbon.
- No bottom strip. No top strip. No corner widgets.
- No menus inside the window (only the native macOS menu bar / GTK header).
- No splash, no tip-of-the-day, no first-run modal.
- No app-drawn scrollbar by default (`scrollbar = system`).

### Design rule

> Every pixel of UI chrome competes with the user's content for attention. Default to no chrome.

This is the rule. Onibi's audit critique (cockpit pills, status bar, identity-at-rest signals) directly violates it — adapting Ghostty's design means **deciding which Onibi chrome is *essential* and which can be removed or made on-demand.**

---

## 3. Typography

| Aspect | Ghostty default | Notes |
|---|---|---|
| Font family | JetBrains Mono (embedded, ships with binary) | Sized to the user's system font scale. |
| Font features | Nerd Fonts patched in by default | Glyphs for Starship/powerline work out of the box. |
| Ligatures | **Off** as of v1.2.0 (was on) | Configurable per-feature via `font-feature`. |
| Font weight | Regular for body; italics/bold use the same family unless overridden | `font-synthetic-style` enabled — fakes bold/italic if the font lacks them. |
| Cell sizing | Adjustable in pixels OR percentages (`adjust-cell-width`, `adjust-cell-height`) | Lets users tune density without changing font size. |
| Underline | Configurable position + thickness | Same for strike / overline. |

**Design rule:** typography is the product. The terminal is text. Defaults should look great on a fresh install with no config; nothing else (theme, padding, blur) should be more confident than the type.

**For Onibi**: the *editor* uses CodeMirror (proportional & monospace) and the *terminal* uses xterm.js. Both should pick **one** monospace family with embedded fallback (JetBrains Mono is a defensible choice), at one size, with no ligatures by default.

---

## 4. Color & theme

### Default color logic

- Ships with **hundreds** of built-in themes (catppuccin, dracula, nord, tokyo-night, gruvbox, solarized, etc.).
- One-line theme selection: `theme = "Catppuccin Mocha"`.
- **Auto-switches with system light/dark mode** when a pair is configured (`theme = light:X,dark:Y`).
- Color palette: 16 base + extended 256, optionally auto-generated from the base 16 (`palette-generate`).

### Palette principles

- High contrast between foreground and background (enforced via `minimum-contrast`, WCAG-aligned).
- Background is solid by default (opacity 1.0). Translucency is opt-in.
- Accent colors used sparingly — selection highlight, search match, cursor.
- Search match: **black-on-gold** (focused) / **black-on-peach** (unfocused). These are bold attention colors, used only for active search hits.

### What Ghostty does NOT do

- No gradient backgrounds (unless you set a background image, which is also opt-in).
- No accent-color decoration around panes / tabs / windows.
- No "pro" / "premium" coloring tier.

**For Onibi**: prune the 19 advertised themes to ~6 well-tested. Make `light:X,dark:Y` auto-switching default behavior. Use a single accent color (`--accent-2`) consistently and sparingly.

---

## 5. Window chrome & layout

### Title bar

- macOS: native title bar with traffic lights, native menu bar (File / Edit / View / etc.).
- macOS option `macos-titlebar-style = tabs` collapses the title bar into the tab strip, saving ~22px of vertical space.
- Linux: native GTK header bar; configurable client-side decoration (`window-decoration = client | server | auto | none`).
- The title bar **shows the current process name or shell prompt** — no application branding, no breadcrumbs, no agent labels, no version pill. It is the system title bar, used as the system intends.

### Tab strip

- Native, not custom-drawn.
- `window-show-tab-bar = auto` (default): tab strip is **hidden when only one tab exists** and appears when a second is opened. This is the single most important UI behavior for staying minimal.
- macOS tabs can be moved into the title bar entirely.
- No tab close-on-hover-only weirdness; native close behavior.

### Padding

| Setting | Default | Common user choice |
|---|---|---|
| `window-padding-x` | 0 | 4–10 |
| `window-padding-y` | 0 | 4–10 |
| `window-padding-balance` | false | true |
| `window-padding-color` | background | (rarely changed) |

**Default of 0** says: the terminal edge is the window edge. No frame. Text begins at the pixel after the window border. Most users add 4–8 points; the *default* is naked.

### Window decoration

- `window-decoration = auto` — defers to the platform.
- Generic Linux can opt to draw the window border itself (`client`) or let the WM (`server`).

---

## 6. Tabs & splits

### Tabs

- Native tab behavior (Cmd+T new, Cmd+W close, Cmd+1..9 jump, Cmd+Shift+] / [ navigate).
- Tab title = current shell process name OR last OSC-2 title from the running program. No app interference.
- No app-supplied tab icons. No agent labels. No status indicators on the tab itself.

### Splits

- Cmd+D = vertical split (right of current).
- Cmd+Shift+D = horizontal split (below current).
- Cmd+[arrow] = move focus.
- Cmd+Shift+Enter = zoom focused pane (toggle).
- `unfocused-split-opacity` default ≈ 0.6 — the unfocused pane is **dimmed by lowering its opacity** instead of drawing a focus ring. The focused pane is brighter; everything else recedes. This is a key Ghostty visual move: focus is conveyed by light, not by lines.
- `split-divider-color` is configurable but the divider itself is a hairline (1px) in a neutral tone.
- `unfocused-split-fill` colors the dimming overlay.

**Design rule:** focus by brightness, not by chrome. Borders/outlines on focused panes are an anti-pattern relative to Ghostty's approach.

---

## 7. Cursor & selection

| Element | Ghostty default | Rationale |
|---|---|---|
| Cursor shape | `block` | Matches terminal convention. |
| Cursor blink | On | Standard expectation. |
| Cursor color | `cell-foreground` (auto from theme) | No hard-coded magenta/whatever. |
| Cursor opacity | Full | But configurable for subtlety. |
| Selection bg | Theme-derived (subtle) | Not a saturated color. |
| Selection auto-clear on typing | True | Selection is ephemeral. |
| Selection auto-clear on copy | False | Lets you paste-then-paste. |
| Search match | Black-on-gold (`#000000` on `#FFD700`-ish) | Reserved for the most-attention case. |

**For Onibi**: ApprovalModal "approve" / "deny" buttons should NOT be saturated red/green if they'd compete with the search-match yellow as the most attention-grabbing color in the app. Reserve the loudest color for the rarest, most-urgent state.

---

## 8. Effects (opacity, blur, background image)

### Opacity + blur

- `background-opacity = 1.0` (default) — solid.
- When users set `0.85`–`0.95`, `background-blur` kicks in (default 20) and produces the **translucent + blurred** look that's become Ghostty's iconic visual.
- `background-opacity-cells = false` — opacity applies to the *window background only*, not to cells with explicit BG colors. This means foreground text and inline highlights remain crisp; only the empty space is translucent.

### Background image

- Supported via `background-image` (PNG/JPEG).
- `background-image-opacity` independent of window opacity.
- `background-image-fit = contain` default, `position = center` default.
- Configurable but **off by default.** Ghostty's identity does not rely on background images.

### What Ghostty avoids

- No animated backgrounds.
- No drop shadows under text.
- No glow effects.
- No "neon" / synthwave default; those exist as themes the user opts into.

**Design rule:** any visual effect is opt-in, never default. The product looks restrained at rest.

---

## 9. Native platform integration (macOS-specific)

These are not skin-deep — they're why Ghostty *feels* native:

| Feature | What it means visually |
|---|---|
| Quick Look (force touch / three-finger tap on text) | macOS preview popover appears over the terminal. No app UI involved. |
| Proxy icon in title bar | The native macOS document-proxy icon shows the current working directory; users can drag it to other apps, click to navigate. |
| Secure keyboard entry indicator | A small lock icon appears in the title bar / status menu when secure input is on. Native macOS API, not app-drawn. |
| AppleScript automation | Scripting works through standard macOS scripting bridge. |
| Window state recovery | After restart, windows reopen at their previous positions, tabs preserved. Same as Safari, TextEdit. |
| System appearance switching | Theme follows system light/dark mode without user intervention. |
| Quick Terminal (`global:cmd+grave_accent=toggle_quick_terminal`) | Drop-down terminal slides from screen edge (like Guake / iTerm hotkey window). Off by default. |

**For Onibi**: Tauri exposes much of this through plugins (`@tauri-apps/plugin-window-state`, `@tauri-apps/plugin-shell`, native menus). Using them — rather than building parallel React components — is what "native" means in Tauri context.

---

## 10. Configuration surface

- Single text file (`~/.config/ghostty/config` or `~/Library/Application Support/com.mitchellh.ghostty/config`).
- HCL-like: `key = value` per line, comments with `#`.
- **No settings GUI.** A theme picker exists (a separate window) but the main config is text.
- Live reload on save.
- `ghostty +show-config` prints the resolved config to stdout.
- Every key has a default; the file can be empty.

**Design rule:** if a feature has more than ~3 user-configurable knobs, expose them as text config, not GUI checkboxes. Settings panes balloon over time; text config doesn't.

---

## 11. Affordances (when chrome IS justified)

Ghostty does add UI chrome in three cases:

1. **Resize overlay** — when you resize the window, a small floating pill shows cell dimensions (e.g., `120 × 36`). Visible only during resize, fades after ~750ms (`resize-overlay-duration`). Configurable position. *Lesson: ephemeral UI for transient information.*

2. **Search bar** — overlay strip at the top during `/`-style search. Yellow/peach highlights show matches. Disappears on Escape. *Lesson: modal-on-demand, not persistent.*

3. **Quick Terminal** — drop-down terminal triggered by global hotkey. Slides in from top/bottom/left/right; covers content; disappears on hotkey re-press. *Lesson: spatial cues (slide direction) carry meaning.*

**Rule:** persistent chrome must be earned. Default to ephemeral / on-demand / OS-native surfaces.

---

## 12. Interaction principles

| Principle | Manifestation |
|---|---|
| **Platform conventions win** | Cmd+T = new tab on macOS, Ctrl+Shift+T on Linux. No app-specific muscle-memory required. |
| **Sensible defaults** | An empty config file gives a great-looking, well-behaved terminal. |
| **Configurability without UI bloat** | Hundreds of options exist as text, none as visible checkboxes in the main window. |
| **Native gestures** | Quick Look gesture, force touch, scroll inertia, swipe — all forwarded to OS. |
| **No "discoverability moments"** | No tooltips popping up. No "did you know?" banners. No coach marks. Documentation exists in `ghostty +help` and online. |
| **Performance is felt, not shown** | No FPS counter, no "buttery smooth!" banner. The product is fast and that's the demo. |
| **Auto-hide by default** | Tab bar auto-hides. Scrollbar uses system style. Resize overlay fades. Nothing lingers unless it must. |

---

## 13. Applying this to Onibi — preview of the gap

Onibi today has (audit baseline, June 2026):

- Activity bar (always visible, ~5 icons).
- Workspace sidebar (always visible by default).
- Title bar with breadcrumb + Cmd+K pill + agent status dot.
- Status bar (transport pill, branch, font size, exit code).
- Approvals tile, cockpit pills, welcome hero.
- 19 themes, multiple settings tabs.

**Ghostty's design rules say to:**

| Onibi current | Ghostty rule | Implication |
|---|---|---|
| Always-visible activity bar | Tab bar auto-hides; chrome must be earned | Activity bar should collapse when no sidebar is open, OR shrink to a thin rail. |
| Persistent status bar | No status bar | Move font-size / transport into ephemeral surfaces (HUD, command palette). Keep only **approval queue** as persistent — it's the one thing that justifies always-on chrome in a *cockpit*. |
| Cmd+K pill in title bar | No tutorial / discoverability moments | The pill is a tutorial element. Keep ⌘K binding, drop the pill OR fade it after first use. |
| 19 themes | Hundreds of themes, but theme picker is a separate surface | Ship many themes; expose them through a focused picker, not the main settings tab. |
| Settings as React panes | Config as text file, GUI is for previews only | Settings.toml is already the source of truth — emphasize editing the file; the GUI is a preview layer. |
| Welcome hero + cockpit pills | No splash / first-run | Cockpit pills are justified for an *approval cockpit*; the hero text is decoration and could be removed. |
| Workspace sidebar collapse via icon | Native window controls and platform conventions | Use ⌘B / Ctrl+B as the *only* affordance; remove the dedicated "collapse sidebar" button. |
| Source Control panel always available | Each surface is single-purpose | Make Source Control a *modal* command-palette flow (`Cmd+K → "Stage all"`), not a persistent tab. |
| Approval modal (current: app-styled) | Native dialog where possible; restrained where not | Approval modal should use platform-native dialog padding/typography conventions; remove app branding from it. |

The **single Ghostty rule that hurts Onibi most**: *every pixel of chrome competes with the user's content for attention.* Onibi is an approval cockpit, so some chrome is earned — but currently many surfaces are decorative, not functional.

---

## 14. Adoption checklist (for the next pass)

Order: cheapest wins first. Each item maps to a concrete code change in the Onibi frontend.

1. **Auto-hide the workspace sidebar by default** (show on first launch or Cmd+B).
2. **Auto-hide the agent rail when no sessions exist** — render an inline "Start a session" affordance in the main pane instead of a permanent rail.
3. **Collapse the StatusBar** to one element: pending approvals. Move transport/git/cwd/font-size to ephemeral pills / HUD overlay / command palette.
4. **Drop the welcome hero** ("Onibi — Local-first approval gate…"). Replace with cockpit pills only.
5. **Drop the Cmd+K pill** in the title bar (keep the binding). Replace with a `⌘K` glyph in the corner that fades on first invocation.
6. **Reduce window padding** to 0 or 4px; let content reach the edges.
7. **Adopt focus-by-opacity** for terminal splits (dim unfocused panes via opacity, remove border-ring on focused).
8. **Reduce themes** from 19 to 6 with `light:X,dark:Y` auto-switching.
9. **Reserve the most saturated accent color** (likely the approvals-pending yellow) for *one* purpose — pending approvals. Use neutral / native chrome elsewhere.
10. **Use platform-native dialogs where possible** (Tauri's `@tauri-apps/plugin-dialog`) for confirmations, file picks, errors — instead of React-rendered modals.
11. **Move git stage/commit/diff workflow into the command palette**, not a persistent sidebar tab.
12. **Approval modal**: native padding (≥16px), platform monospace font, no app branding, no logo, no "Approve" emoji — text only.

---

## 15. Specific tokens to adopt

These are concrete numerical / color values to copy into Onibi's CSS / theme system. [Inference] derived from defaults + community configs; substitute with theme variables in implementation.

```text
font-family            JetBrains Mono, ui-monospace, monospace
font-size              13–14pt (system-scaled)
line-height            1.2 (cell-height adjusted)
font-feature-settings  "calt" off (no ligatures)
window-padding         4 4 (x, y) — minimal, not zero
unfocused-opacity      0.6 (split panes), 0.85 (sidebar when peripheral)
divider-color          rgba(255,255,255,0.06) on dark; rgba(0,0,0,0.08) on light
selection-bg           theme.accent + 35% alpha
search-match-bg        #f6c25d (gold)
search-match-fg        #1a1a1a
search-match-focused-bg #f6a878 (peach)
cursor-style           block
cursor-blink           on
scrollbar              system (let the OS draw it)
resize-overlay-ms      750
```

---

## 16. Sources

- [Ghostty official site](https://ghostty.org/)
- [Ghostty About docs](https://ghostty.org/docs/about)
- [Ghostty Features](https://ghostty.org/docs/features)
- [Ghostty Config Reference](https://ghostty.org/docs/config/reference)
- [Mitchell Hashimoto — Ghostty page](https://mitchellh.com/ghostty)
- [Terminal Trove interview with Mitchell Hashimoto](https://terminaltrove.com/blog/terminal-trove-talks-with-mitchell-hashimoto-ghostty/)
- [Ghostty + Quick Terminal — community guide](https://azerkoculu.com/posts/ghostty-quick-terminal)
- [Minimal Ghostty Config — Samuel Lawrentz](https://samuellawrentz.com/blog/minimal-ghostty-config/)
- [GitHub ghostty-org/ghostty](https://github.com/ghostty-org/ghostty)
