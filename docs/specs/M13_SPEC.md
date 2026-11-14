# Milestone M13 Specification: Design System Tokens & Component Stylesheets (`v2-ux`)

## 1. Objective
Extract and upgrade the ACN Protocol Console stylesheets into modular CSS files under `internal/api/ui/`:
1. `internal/api/ui/tokens.css` — Sovereign Cryptographic Trust-and-Escrow Mission Control design tokens (`:root` custom properties).
2. `internal/api/ui/components.css` — Layout, semantic landmarks, WAI-ARIA tabs, 5-state banners, `CLS = 0` skeletons, tables, forms, stepper, jury fallback callout, reduced-motion, and responsive breakpoints (`375px` to `2560px`).
3. `internal/api/ui_assets_test.go` — Go unit tests validating the presence, token definitions, `tabular-nums` rules, reduced-motion media queries, and zero-emoji invariant of `tokens.css` and `components.css`.

## 2. Target Files
1. `internal/api/ui/tokens.css`
2. `internal/api/ui/components.css`
3. `internal/api/ui_assets_test.go`

## 3. Detailed Contract for `internal/api/ui/tokens.css`
Must define `:root` CSS custom properties matching `docs/DESIGN_SYSTEM.md`:
- **4-Tier Surface Elevation Hierarchy**:
  - `--bg-canvas: #070b12;`
  - `--bg-surface-1: #0d131f;`
  - `--bg-surface-2: #131c2e;`
  - `--bg-surface-3: #1a263d;`
  - `--bg-input: #090e17;`
  - `--bg-code: #05080e;`
  - Legacy aliases: `--bg-dark: var(--bg-canvas);`, `--bg-panel: var(--bg-surface-1);`, `--bg-card: var(--bg-surface-2);`
- **Hairline Borders & Focus Rings**:
  - `--border-subtle: #1b263b;`
  - `--border-default: #24324d;`
  - `--border-strong: #33476b;`
  - `--border-focus: #38bdf8;`
  - `--focus-ring: 0 0 0 2px rgba(56, 189, 248, 0.45);`
  - Legacy aliases: `--border-color: var(--border-default);`, `--border-active: var(--border-focus);`
- **Typography & Contrast Hierarchy (WCAG 2.2 AA `>= 4.5:1`)**:
  - `--text-main: #f1f5f9;`
  - `--text-muted: #94a3b8;`
  - `--text-dim: #8b9bb4;`
  - `--text-on-accent: #04101e;`
  - `--font-sans: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Inter", Helvetica, Arial, sans-serif;`
  - `--font-mono: ui-monospace, SFMono-Regular, "JetBrains Mono", Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace;`
  - `--text-2xs: 0.625rem;`, `--text-xs: 0.6875rem;`, `--text-sm: 0.75rem;`, `--text-base: 0.8125rem;`, `--text-md: 0.875rem;`, `--text-lg: 1.125rem;`, `--text-xl: 1.25rem;`
- **Primary Accents & Semantic Trust/Status Tokens**:
  - `--accent-blue: #38bdf8;`, `--accent-blue-hover: #7dd3fc;`, `--accent-green: #4ade80;`, `--accent-amber: #fbbf24;`, `--accent-red: #f87171;`, `--accent-purple: #c084fc;`
  - `--badge-t0-bg: #162032;`, `--badge-t0-fg: #cbd5e1;`, `--badge-t0-border: #334155;`
  - `--badge-t1-bg: #082f49;`, `--badge-t1-fg: #38bdf8;`, `--badge-t1-border: #0369a1;`
  - `--badge-t2-bg: #2e1065;`, `--badge-t2-fg: #d8b4fe;`, `--badge-t2-border: #6b21a8;`
  - `--badge-t3-bg: #052e16;`, `--badge-t3-fg: #4ade80;`, `--badge-t3-border: #166534;`
  - `--badge-vendor-bg: #451a03;`, `--badge-vendor-fg: #fdba74;`, `--badge-vendor-border: #9a3412;`
  - `--status-ok-bg: #052e16;`, `--status-ok-fg: #4ade80;`, `--status-ok-border: #166534;`
  - `--status-danger-bg: #450a0a;`, `--status-danger-fg: #fca5a5;`, `--status-danger-border: #991b1b;`
  - `--status-warn-bg: #451a03;`, `--status-warn-fg: #facc15;`, `--status-warn-border: #854d0e;`
  - `--status-degraded-bg: #2e1905;`, `--status-degraded-fg: #fbbf24;`, `--status-degraded-border: #d97706;`
  - `--status-info-bg: #082f49;`, `--status-info-fg: #7dd3fc;`, `--status-info-border: #0369a1;`
- **Spatial Scale & Motion**:
  - `--space-1: 0.25rem;`, `--space-2: 0.5rem;`, `--space-3: 0.75rem;`, `--space-4: 1rem;`, `--space-5: 1.25rem;`, `--space-6: 1.5rem;`, `--space-8: 2rem;`
  - `--radius-sm: 4px;`, `--radius-md: 6px;`, `--radius-lg: 8px;`
  - `--ease-out-expo: cubic-bezier(0.16, 1, 0.3, 1);`, `--duration-fast: 140ms;`

## 4. Detailed Contract for `internal/api/ui/components.css`
- Must use CSS variables from `tokens.css` exclusively (zero raw `#hex` colors in `components.css`).
- Must define:
  - `.sr-only` (screen-reader only utility) and `.skip-link` (visible on `:focus` / `:focus-visible`).
  - `:focus-visible` rule with `outline: none; border-color: var(--border-focus); box-shadow: var(--focus-ring);`.
  - `.tabular-nums`, `.stat-val`, `.pill-val`, `.code-span`, `.badge`, `.step-circle`, `td.num`, `th.num`, `#orc-nonce-timer` with `font-variant-numeric: tabular-nums;`.
  - Semantic layout: `body`, `header`, `.brand`, `.top-telemetry`, `.pill`, `.pill-label`, `.pill-val` (`.green`, `.blue`, `.amber`), `nav.tab-bar`, `.tab-btn` (`[aria-selected="true"]` and `.active`), `main`, `.tab-content` (`[hidden]` and `.active`), `.grid-2`, `.grid-3`, `.grid-4`, `.card`, `.card-title`, `.stat-box`, `.stat-label`, `.stat-val`, `footer`.
  - Tables & Badges: `.table-responsive`, `table`, `th`, `td`, `.badge`, `.badge-t0`, `.badge-t1`, `.badge-t2`, `.badge-t3`, `.badge-vendor`, `.badge-success`, `.badge-danger`, `.badge-warning`, `.badge-degraded`, `.code-span`.
  - Forms & Buttons: `.filter-toolbar`, `.filter-group`, `.form-group`, `.form-control`, `.form-row`, `.form-check`, `.btn`, `.btn-secondary`, `.btn-danger`, `.btn-sm`, `pre.json-viewer`.
  - 5-State Status Banner & Skeletons: `.status-banner` (`[data-state="empty"]`, `[data-state="loading"]`, `[data-state="nominal"]`, `[data-state="degraded"]`, `[data-state="error"]`), `.skeleton-block`, `.skeleton-row`, `@keyframes acn-skeleton-pulse`.
  - Stepper, Jury Callout & Attack Lab: `.stepper`, `.step-node` (`.completed`, `.active`, `.failed`), `.step-circle`, `.step-title`, `.jury-callout` (`[data-engine="DETERMINISTIC_MULTI_FACTOR"]`), `.attack-card`, `.attack-header`, `.attack-title`, `.attack-desc`, `.attack-defense`.
  - Motion & Responsive Queries: `@media (prefers-reduced-motion: reduce)`, `@media (max-width: 1023px)`, `@media (max-width: 767px)`, `@media (min-width: 1600px)`.

## 5. Detailed Contract for `internal/api/ui_assets_test.go`
- Package `api_test` (or `api`) testing `internal/api/ui/tokens.css` and `internal/api/ui/components.css` using `os.ReadFile`:
  - Verifies `tokens.css` defines `--bg-canvas`, `--bg-surface-1`, `--bg-surface-2`, `--bg-surface-3`, `--text-dim: #8b9bb4`, `--badge-t0-bg`..`--badge-t3-bg`, `--status-degraded-bg`, `--font-sans`, `--font-mono`, and `--focus-ring`.
  - Verifies `components.css` defines `.skip-link`, `.sr-only`, `font-variant-numeric: tabular-nums`, `.status-banner[data-state="degraded"]`, `.skeleton-row`, `.jury-callout`, `@media (prefers-reduced-motion: reduce)`, and contains zero raw `#` hex literals.
- Every test function has a structured Go doc comment with `Pre-conditions:` and `Post-conditions:`.

## 6. Verification Command
```bash
export PATH="/home/tron/.local/go/bin:$PATH" && test -z "$(gofmt -l .)" && go vet ./... && go test -v -race ./internal/api/...
```
