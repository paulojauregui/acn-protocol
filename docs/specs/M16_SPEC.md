# Milestone M16 Specification: Modular JS Core View Renderers & 5-State Skeletons (`v2-ux`)

## 1. Objective
Implement `internal/api/ui/views.js` providing pure, XSS-safe DOM rendering helpers, `CLS = 0` skeleton generators, 5-state status banner updaters, wire-protocol payload normalization (`sub.sub_did || sub.sub_agent_did` and `session.state || session.status`), and jury fallback callout rendering (`DETERMINISTIC_MULTI_FACTOR` vs `DGX_SPARK_LLM`), along with `internal/api/ui_test.go` verifying asset serving and contract invariants.

## 2. Target Files
1. `internal/api/ui/views.js`
2. `internal/api/ui_test.go`

## 3. Detailed Contract for `internal/api/ui/views.js`
Must expose deterministic global helper functions (consumable by `app.js` and inline handlers) with zero emojis:
1. `escapeHTML(str)`: Escapes `&`, `<`, `>`, `"`, `'` safely.
2. `fmtUSD(val)`: Formats numbers as `$X,XXX.XX`.
3. `announceSR(message)`: Updates `#sr-announcer` textContent for screen-reader polite announcements.
4. `setBannerState(elementId, state, message, isAlert)`:
   - Updates `#${elementId}` (`data-state` = `"empty"` | `"loading"` | `"nominal"` | `"degraded"` | `"error"`).
   - Sets `role="alert"` when `state === "error"` or `isAlert === true`, otherwise `role="status"`.
   - Sets `aria-busy="true"` when `state === "loading"`, otherwise `"false"`.
   - Removes `hidden` attribute and updates content, plus announces via `announceSR`.
5. `renderTableSkeleton(tbodyId, colCount, rowCount)`:
   - Sets `aria-busy="true"` on `#${tbodyId}` and renders `rowCount` `<tr class="skeleton-row" data-state="loading">` rows with `colCount` `<td></td>` cells (`CLS = 0`).
6. `getTierBadge(tier, isVendor)`:
   - Returns `.badge-vendor`, `.badge-t3`, `.badge-t2`, `.badge-t1`, or `.badge-t0` span HTML.
7. `renderAgentsTable(rootItems, filterQuery)`:
   - Sets `aria-busy="false"` on `#agents-table-body`.
   - Filters `rootItems` case-insensitively by `display_name`, `root_did`, or tier/vendor label if `filterQuery` is non-empty.
   - If 0 items match, renders a `<tr data-state="empty">` row with `"No agents match filter criteria"` and a `<button type="button" class="btn btn-secondary btn-sm" onclick="clearAgentFilter()">Reset Filter</button>`.
   - Renders each agent row with `class="tabular-nums"` / `td.num` on numeric columns (`bond_usd`, `cumulative_spend_usd`, `settled_tx_count`, `composite_score`) and a `Bond` button calling `selectAgentForDeposit(root_did)`.
8. `renderSubAgentsTable(subAgents)`:
   - Sets `aria-busy="false"` on `#subagents-table-body`.
   - If empty, renders `<tr data-state="empty">` guided empty state.
   - Normalizes `const subDID = sub.sub_did || sub.sub_agent_did || '';` so real Go API `SubAgent` payloads (`json:"sub_did"`) render accurately.
9. `populateAgentDropdowns(rootItems, subAgents)`:
   - Preserves current selection where possible and populates `#spawn-parent`, `#deposit-did`, `#pass-subject`, `#relay-caller`, `#orc-user-did`, `#relay-vendor`, and `#orc-vendor-did` (normalizing `s.sub_did || s.sub_agent_did`).
10. `setStepperState(state)`:
    - Updates `#step-1`..`#step-4` classes (`active`, `completed`, `failed`, or idle).
11. `renderJuryCallout(verdict)`:
    - Updates `#jury-fallback-callout`. If `!verdict`, sets `hidden` and `data-state="empty"`.
    - If `verdict.engine_used === "DETERMINISTIC_MULTI_FACTOR"` or `"deterministic"`, removes `hidden`, sets `data-state="degraded"` and `data-engine="DETERMINISTIC_MULTI_FACTOR"`, and renders a `.badge-degraded` badge (`DETERMINISTIC_MULTI_FACTOR FALLBACK`) with the rationale explaining that local multi-factor heuristics resolved the dispute without remote DGX Spark LLM dependency.
    - If `verdict.engine_used === "DGX_SPARK_LLM"`, removes `hidden`, sets `data-state="nominal"` and `data-engine="DGX_SPARK_LLM"`, and renders a `.badge-success` badge (`DGX_SPARK_LLM ARBITRATED`) with the rationale.
12. `renderSimMetrics(scenario, metrics)`:
    - Sets `aria-busy="false"` on `#sim-metrics-tbody` and renders the 5 mathematical invariant rows for `sybil-swarm`, `whale-attack`, or `collusive-vendor` with `.tabular-nums` on observed values.

## 4. Detailed Contract for `internal/api/ui_test.go`
- Package `api_test` with Go doc comments (`Pre-conditions:` and `Post-conditions:`) on every test function.
- Verifies `GET /ui/views.js` returns `200 OK` with `Content-Type: application/javascript; charset=utf-8` and contains required functions (`escapeHTML`, `fmtUSD`, `setBannerState`, `renderTableSkeleton`, `renderAgentsTable`, `renderSubAgentsTable`, `renderJuryCallout`, `renderSimMetrics`, `sub.sub_did`, `DETERMINISTIC_MULTI_FACTOR`) and zero `window.alert(` or emojis.

## 5. Verification Command
```bash
export PATH="/home/tron/.local/go/bin:$PATH" && test -z "$(gofmt -l .)" && go vet ./... && go test -v -race ./internal/api/...
```
