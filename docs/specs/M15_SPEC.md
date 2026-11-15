# Milestone M15 Specification: Semantic HTML5 5-Tab Console Shell (`v2-ux`)

## 1. Objective
Refactor `internal/api/ui/index.html` from a 1,946-line inline CSS+HTML+JS monolith into a clean, semantic HTML5 shell (`< 500` lines) that links `/ui/tokens.css`, `/ui/components.css`, `/ui/views.js`, and `/ui/app.js`, and create `internal/api/ui_html_test.go` to verify all WCAG 2.2 AA landmarks, WAI-ARIA tabs, explicit `<label for="...">` bindings, live status regions, and zero inline `<style>` or inline `<script>` blocks.

## 2. Target Files
1. `internal/api/ui/index.html`
2. `internal/api/ui_html_test.go`

## 3. Detailed Contract for `internal/api/ui/index.html`
- **External Assets Only (Zero Inline `<style>` or Inline `<script>` Code)**:
  - In `<head>`:
    ```html
    <link rel="stylesheet" href="/ui/tokens.css">
    <link rel="stylesheet" href="/ui/components.css">
    ```
  - Before `</body>`:
    ```html
    <script src="/ui/views.js" defer></script>
    <script src="/ui/app.js" defer></script>
    ```
  - Zero `<style>` tags in `index.html`.
- **Semantic Landmarks & Skip Navigation**:
  - `<a href="#main-content" class="skip-link">Skip to main console</a>`
  - `<div id="sr-announcer" class="sr-only" role="status" aria-live="polite" aria-atomic="true"></div>`
  - `<header role="banner">` containing `.brand` (`<h1>ACN Protocol Engineering Console</h1>`) and `.top-telemetry` with `#pill-status` (`data-state="nominal"`), `#pill-merkle-root`, `#pill-registered-agents`, `#pill-sub-agents`, `#pill-escrow-locked`, `#pill-treasury-fees`, and `#btn-refresh-overview` (`onclick="refreshOverview()"`).
  - `<nav class="tab-bar" id="console-tablist" role="tablist" aria-label="Console Primary Views">` containing 5 `<button type="button" class="tab-btn">` elements:
    1. `id="tab-btn-overview" role="tab" aria-selected="true" aria-controls="tab-overview" tabindex="0"` (`onclick="switchTab('tab-overview', this)"`)
    2. `id="tab-btn-relay" role="tab" aria-selected="false" aria-controls="tab-relay" tabindex="-1"` (`onclick="switchTab('tab-relay', this)"`)
    3. `id="tab-btn-oracle" role="tab" aria-selected="false" aria-controls="tab-oracle" tabindex="-1"` (`onclick="switchTab('tab-oracle', this)"`)
    4. `id="tab-btn-raas" role="tab" aria-selected="false" aria-controls="tab-raas" tabindex="-1"` (`onclick="switchTab('tab-raas', this)"`)
    5. `id="tab-btn-threat" role="tab" aria-selected="false" aria-controls="tab-threat" tabindex="-1"` (`onclick="switchTab('tab-threat', this)"`)
  - `<main id="main-content" role="main" tabindex="-1">` containing 5 `<section>` panels with `role="tabpanel"`:
    1. `<section id="tab-overview" class="tab-content active" role="tabpanel" aria-labelledby="tab-btn-overview">`
    2. `<section id="tab-relay" class="tab-content" role="tabpanel" aria-labelledby="tab-btn-relay" hidden>`
    3. `<section id="tab-oracle" class="tab-content" role="tabpanel" aria-labelledby="tab-btn-oracle" hidden>`
    4. `<section id="tab-raas" class="tab-content" role="tabpanel" aria-labelledby="tab-btn-raas" hidden>`
    5. `<section id="tab-threat" class="tab-content" role="tabpanel" aria-labelledby="tab-btn-threat" hidden>`
  - `<footer role="contentinfo">`.
- **Mandatory Explicit `<label for="...">` Bindings on All 28 Form Controls**:
  - Tab 1 (`#tab-overview`):
    - Quick Filter toolbar (`.filter-toolbar`): `<label for="agent-filter-input">` + `<input type="search" class="form-control" id="agent-filter-input" placeholder="Filter by name, DID, or tier (/ or Ctrl+K)...">` + `<button type="button" class="btn btn-secondary btn-sm" id="btn-clear-filter" onclick="clearAgentFilter()">Clear Filter</button>`
    - Register Root Agent: `<label for="reg-name">`, `<label for="reg-vendor">`, `<label for="reg-bond">`, `<label for="reg-pop">`, `<label for="reg-tee">`
    - Spawn Sub-Agent: `<label for="spawn-parent">`, `<label for="spawn-purpose">`, `<label for="spawn-ttl">`
    - Deposit Bond: `<label for="deposit-did">`, `<label for="deposit-amount">`
  - Tab 2 (`#tab-relay`):
    - `<label for="relay-caller">`, `<label for="relay-vendor">`, `<label for="relay-service">`, `<label for="relay-bond">`, `<label for="relay-min-score">`, `<label for="relay-min-tier">`
  - Tab 3 (`#tab-oracle`):
    - `<label for="orc-appt-id">`, `<label for="orc-user-did">`, `<label for="orc-vendor-did">`, `<label for="settle-lock-id">`, `<label for="settle-amount">`, `<label for="settle-user-pres">`, `<label for="settle-vendor-pres">`, `<label for="settle-stripe">`, `<label for="settle-beacon">`, `<label for="settle-rating">`
  - Tab 4 (`#tab-raas`):
    - `<label for="pass-subject">`, `<label for="pass-min-ful">`, `<label for="pass-min-book">`, `<label for="pass-min-tier">`, `<label for="merkle-proof-idx">`
- **Live Status & 5-State Containers**:
  - `#directory-status-banner` (`class="status-banner" role="status" aria-live="polite" data-state="nominal"`)
  - `#relay-result-summary` (`class="status-banner" role="status" aria-live="polite" data-state="empty"`)
  - `#orc-nonce-status` (`role="status" aria-live="polite" data-state="empty"`)
  - `#settle-result-summary` (`class="status-banner" role="status" aria-live="polite" data-state="empty"`)
  - `#jury-fallback-callout` (`class="jury-callout" role="status" aria-live="polite" data-state="empty" hidden`)
  - `#passport-status-banner` (`class="status-banner" role="status" aria-live="polite" data-state="empty"`)
  - `#merkle-proof-status` (`class="status-banner" role="status" aria-live="polite" data-state="empty"`)
  - `#sim-summary` (`class="status-banner" role="status" aria-live="polite" data-state="empty"`)
- Preserve all canonical DOM IDs from `docs/UX_SPEC.md` (`#stat-locked-usd`, `#stat-avail-usd`, `#stat-fees-usd`, `#stat-slashed-usd`, `#agents-table`, `#agents-table-body`, `#subagents-table-body`, `#relay-stepper`, `#step-1`..`#step-4`, `#relay-badge`, `#relay-json`, `#relay-send-to-oracle`, `#orc-nonce-display`, `#orc-nonce-timer`, `#settle-status-badge`, `#settle-json`, `#passport-json`, `#verify-passport-btn`, `#merkle-ledger-root`, `#merkle-proof-json`, `#merkle-records-tbody`, `#sim-defense-badge`, `#sim-metrics-table`, `#sim-metrics-tbody`, `#sim-json`).

## 4. Detailed Contract for `internal/api/ui_html_test.go`
- Package `api_test` with Go doc comments (`Pre-conditions:` and `Post-conditions:`) on every test function.
- Tests:
  - `TestUIHTML_ExternalAssetLinksAndNoInlineBloat`: Verifies `index.html` links `/ui/tokens.css`, `/ui/components.css`, `/ui/views.js`, `/ui/app.js`, has zero `<style>` tags, and is under 600 lines.
  - `TestUIHTML_LandmarksAndWAIARIATabs`: Verifies `.skip-link`, `#sr-announcer`, `role="banner"`, `role="tablist"`, all 5 `role="tab"` buttons (`#tab-btn-overview`..`#tab-btn-threat`) with `aria-selected`, `aria-controls`, `tabindex`, and all 5 `role="tabpanel"` sections (`#tab-overview`..`#tab-threat`) with `aria-labelledby`.
  - `TestUIHTML_ExplicitFormLabels`: Verifies all 28 form control IDs (`agent-filter-input`, `reg-name`, `reg-vendor`, `reg-bond`, `reg-pop`, `reg-tee`, `spawn-parent`, `spawn-purpose`, `spawn-ttl`, `deposit-did`, `deposit-amount`, `relay-caller`, `relay-vendor`, `relay-service`, `relay-bond`, `relay-min-score`, `relay-min-tier`, `orc-appt-id`, `orc-user-did`, `orc-vendor-did`, `settle-lock-id`, `settle-amount`, `settle-user-pres`, `settle-vendor-pres`, `settle-stripe`, `settle-beacon`, `settle-rating`, `pass-subject`, `pass-min-ful`, `pass-min-book`, `pass-min-tier`, `merkle-proof-idx`) have a matching `<label for="ID">` in `index.html`.
  - `TestUIHTML_LiveRegionsAndCanonicalIDs`: Verifies `#directory-status-banner`, `#relay-result-summary`, `#settle-result-summary`, `#jury-fallback-callout`, `#passport-status-banner`, `#merkle-proof-status`, and `#sim-summary` exist with `aria-live="polite"`.

## 5. Verification Command
```bash
export PATH="/home/tron/.local/go/bin:$PATH" && test -z "$(gofmt -l .)" && go vet ./... && go test -v -race ./internal/api/...
```
