# Milestone M10 Specification: HTTP/2 REST & Connect-Style RPC Control-Plane API Server (`internal/api`)

## 1. Objective
Implement the HTTP/2 REST and Connect-style RPC control-plane server (`internal/api/server.go`, `internal/api/handlers.go`, and `internal/api/server_test.go`) with deterministic pre-seeded network state across Tiers 0–3, Rotating Sub-Agents, Merchant Vendors (`Autonomous Waymo Fleet SF`, `UCSF Medical Screening`, `Michelin Dining Agent`), settled Merkle batches, and active governance proposals.

## 2. Target Files
1. `internal/api/server.go`
2. `internal/api/handlers.go`
3. `internal/api/server_test.go`

## 3. Design-by-Contract & Architectural Invariants
- **Every exported struct, interface, constant, function, and method MUST have a Go doc comment (`// SymbolName ...`) that explicitly states `Pre-conditions:` and `Post-conditions:`.**
- **Zero emojis** in code or comments.
- Serve embedded UI HTML at `GET /` (with a fallback HTML placeholder until `internal/api/ui/index.html` is embedded in `M11`, or using `//go:embed ui/index.html`).

## 4. Exact Endpoints & Pre-Seeded State

### 4.1 `internal/api/server.go`
- `type Server struct` holding `*identity.Registry`, `*reputation.Engine`, `*escrow.Vault`, `*oracle.Oracle`, `*ledger.Ledger`, `*governance.Engine`, `*governance.Jury`, `*relay.Router`, and `*http.ServeMux`.
- `func NewServer() *Server`
  - Initializes all domain engines and calls `seedInitialState()` to register:
    - **Sovereign User Agents across Tiers 0–3**:
      - `Alice Sovereign Executive` (`Tier 3 Sovereign`, `PoPVerified: true`, `TEEAttested: true`, `BondUSD: 500`, `CumulativeSpendUSD: 2400`, `SettledTxCount: 35`, `CompositeScore: 4.85`) + 2 rotating Sub-Agents.
      - `Bob Reputational Traveler` (`Tier 2 Reputational`, `BondUSD: 150`, `CumulativeSpendUSD: 750`, `SettledTxCount: 22`, `CompositeScore: 4.10`) + 1 rotating Sub-Agent.
      - `Charlie Economic Commuter` (`Tier 1 Economic`, `BondUSD: 60`, `CumulativeSpendUSD: 120`, `SettledTxCount: 6`, `CompositeScore: 3.40`).
      - `Eve Unbonded Ephemeral Bot` (`Tier 0 Ephemeral`, `BondUSD: 0`, `CumulativeSpendUSD: 0`, `SettledTxCount: 0`, `CompositeScore: 2.10`).
    - **Merchant Vendor Agents**:
      - `Autonomous Waymo Fleet SF` (`IsVendor: true`, `BondUSD: 5000`, `CompositeScore: 4.92`)
      - `UCSF Medical Screening` (`IsVendor: true`, `BondUSD: 10000`, `CompositeScore: 4.96`)
      - `Michelin Dining Agent` (`IsVendor: true`, `BondUSD: 2500`, `CompositeScore: 4.88`)
    - **Initial Settled Merkle Ledger Records** (4+ historical records anchored in `ledger.Ledger`).
    - **Initial Governance Proposals** (2 active constitutional proposals).
- `func (s *Server) Handler() http.Handler`

### 4.2 `internal/api/handlers.go`
- `GET /healthz` -> `200 OK` `{"status":"healthy","service":"acn-protocol","version":"1.0.0"}`
- `GET /ready` -> `200 OK` `{"status":"ready","merkle_root":"...","registered_agents":...}`
- `GET /api/v1/overview` -> Network telemetry, current `merkle_root`, active Root Agents & Sub-Agents count, escrow `total_locked_usd`, `total_available_usd`, `treasury_fees_usd`, `total_slashed_usd`, and recent sessions/outcomes.
- `GET /api/v1/agents` -> Lists all `RootAgent`s (with their escrow account & reputation snapshot) and `SubAgent`s.
- `POST /api/v1/agents` -> Supports actions `"register_root"`, `"spawn_sub"`, or `"deposit_bond"` (default `"register_root"` if `action` omitted).
- `POST /api/v1/relay/book` -> Executes `router.NegotiateBooking` (`CallerDID`, `VendorDID`, `ServiceName`, `BondAmountUSD`, `MinRequiredScore`, `RequiredTrustTier`).
- `POST /api/v1/oracle/challenge` -> Issues a 60s `ChallengeNonce` via `oracle.IssueChallengeNonce`.
- `POST /api/v1/oracle/settle` -> Executes Double-Blind settlement (`oracle.SettleAppointment`), updates `escrow.Vault` (`ReleaseBond` on `STATUS_SUCCESS`, `SlashBond` on `STATUS_SLASHED` or when `STATUS_DISPUTED` finds user at fault), runs `governance.Jury.ArbitrateDispute` on `STATUS_DISPUTED`, updates `identity.Registry` & `reputation.Engine`, and appends the signed record to `ledger.Ledger`.
- `POST /api/v1/raas/passport` & `POST /api/v1/raas/verify` -> Issues and verifies selective-disclosure `TrustPassport` credentials.
- `GET /api/v1/ledger/merkle` -> Returns `merkle_root`, `records`, and an optional `inclusion_proof` for `?index=0`.
- `GET /api/v1/governance/proposals` & `POST /api/v1/governance/vote` -> Lists proposals and casts Reputation-Weighted ($V_i = \ln(1+S_i) \times R_i$) votes.
- `POST /api/v1/simulate/{scenario}` -> Runs `sybil-swarm`, `whale-attack`, or `collusive-vendor`.

### 4.3 `internal/api/server_test.go`
- Comprehensive HTTP integration tests using `net/http/httptest` covering every endpoint above.

## 5. Verification Command
```bash
export PATH="/home/tron/.local/go/bin:$PATH" && test -z "$(gofmt -l internal/api)" && go vet ./internal/api/... && go test -v -race -cover ./internal/api/...
```
