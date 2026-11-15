# Milestone M17 Specification: Modular JS Application Controller, Keyboard Navigation & API Client (`v2-ux`)

## 1. Objective
Implement `internal/api/ui/app.js` containing the application state store, WAI-ARIA roving tabindex keyboard navigation (`ArrowLeft`, `ArrowRight`, `Home`, `End`), global quick filter keyboard shortcuts (`/`, `Cmd/Ctrl+K`, `Escape`), 60s challenge nonce countdown timer, and non-destructive `fetch()` handlers for all 5 console tabs (zero `window.alert()` calls), and expand `internal/api/ui_test.go` to verify `/ui/app.js` and `/ui/views.js` end-to-end.

## 2. Target Files
1. `internal/api/ui/app.js`
2. `internal/api/ui_test.go`

## 3. Detailed Contract for `internal/api/ui/app.js`
- Zero emojis and zero `alert(` / `window.alert(` calls anywhere.
- **State Store**:
  - Tracks `cachedRootAgents`, `cachedSubAgents`, `activeFilterQuery`, `activeNonceInterval`, `activeNonceValue`, `lastConfirmedRelaySession`, `lastIssuedPassport`, `currentMerkleRoot`.
- **WAI-ARIA Tab Switching & Keyboard Navigation**:
  - `switchTab(tabId, btn, focusBtn)`:
    - Updates all `.tab-btn[role="tab"]`: sets `aria-selected="true"`, `tabindex="0"`, `.active` on the selected tab button; sets `aria-selected="false"`, `tabindex="-1"`, removes `.active` on all other tab buttons.
    - Updates all `.tab-content[role="tabpanel"]`: removes `hidden` and adds `.active` on `#${tabId}`; sets `hidden` and removes `.active` on all other tab panels.
    - If `focusBtn === true`, calls `btn.focus()`.
    - Announces the active tab via `announceSR`.
  - Attaches `keydown` listener on `#console-tablist`:
    - `ArrowRight`: activates and focuses `(currentIndex + 1) % tabs.length`.
    - `ArrowLeft`: activates and focuses `(currentIndex - 1 + tabs.length) % tabs.length`.
    - `Home`: activates and focuses `tabs[0]`.
    - `End`: activates and focuses `tabs[tabs.length - 1]`.
- **Global Keyboard Shortcuts (`/`, `Cmd/Ctrl+K`, `Escape`)**:
  - Pressing `/` when `document.activeElement` is not an `INPUT`, `SELECT`, `TEXTAREA`, or `isContentEditable`, or pressing `(e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k'`:
    - Calls `e.preventDefault()`, activates Tab 1 (`switchTab('tab-overview', document.getElementById('tab-btn-overview'))`), and focuses `#agent-filter-input`.
  - Pressing `Escape` inside `#agent-filter-input` (or when `activeFilterQuery` is non-empty):
    - Calls `clearAgentFilter()`.
  - `clearAgentFilter()`: clears `#agent-filter-input`, resets `activeFilterQuery = ""`, re-renders `renderAgentsTable(cachedRootAgents, "")`, and updates `#directory-status-banner`.
- **API Handlers Across All 5 Tabs**:
  - `refreshOverview()`, `fetchAgents()`, `selectAgentForDeposit(did)`, `handleRegisterAgent(e)`, `handleSpawnSubAgent(e)`, `handleDepositBond(e)`.
  - `handleRelayBooking(e)`: normalizes `const sessionState = session.state || session.status || '';`, handles `CONFIRMED` (`data-state="nominal"`), `REJECTED_LOW_TRUST` (`data-state="degraded"`), and HTTP errors (`data-state="error"`) via `setBannerState('relay-result-summary', ...)`.
  - `prefillOracleFromRelay()`: transfers `lock_id`, `bond_amount_usd`, `caller_did`, `vendor_did` to Tab 3 and switches to `#tab-oracle`.
  - `handleIssueNonce(e)`, `startNonceTimer(seconds)`, `setSettlementScenario(type)`, `handleOracleSettle(e)`:
    - Updates `#settle-status-badge`, `#settle-result-summary` (`data-state`), and calls `renderJuryCallout(data.verdict)` so `DETERMINISTIC_MULTI_FACTOR` vs `DGX_SPARK_LLM` is prominently displayed.
  - `handleIssuePassport(e)`, `handleVerifyPassport()`, `fetchMerkleLedger()`, `auditMerkleProof()`:
    - Updates `#passport-status-banner` and `#merkle-proof-status` with 5-state determinism (`empty`, `loading`, `nominal`, `degraded`, `error`).
  - `runSimulation(scenario)`:
    - Renders `renderTableSkeleton('sim-metrics-tbody', 4, 5)` during load, calls `POST /api/v1/simulate/${scenario}`, updates `#sim-defense-badge`, `#sim-summary` (including degraded note when `collusive-vendor` uses deterministic jury fallback), and calls `renderSimMetrics(scenario, data.metrics)`.

## 4. Detailed Contract for `internal/api/ui_test.go`
- Verifies `GET /ui/app.js` returns `200 OK` with `Content-Type: application/javascript; charset=utf-8`.
- Verifies `app.js` implements `switchTab`, `ArrowRight`, `ArrowLeft`, `Home`, `End`, `clearAgentFilter`, `refreshOverview`, `handleRelayBooking`, `handleIssueNonce`, `handleOracleSettle`, `handleIssuePassport`, `handleVerifyPassport`, `fetchMerkleLedger`, `auditMerkleProof`, `runSimulation`, `session.state || session.status`, zero `alert(`, and zero emojis.

## 5. Verification Command
```bash
export PATH="/home/tron/.local/go/bin:$PATH" && test -z "$(gofmt -l .)" && go vet ./... && go test -v -race ./internal/api/...
```
