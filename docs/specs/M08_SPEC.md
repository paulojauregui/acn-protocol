# Milestone M08 Specification: High-Concurrency Agent Relay & SIP-Style Handshake Router (`internal/relay`)

## 1. Objective
Implement the SIP-style Agent-to-Agent Call & Booking Negotiation state machine (`INITIATED -> REPUTATION_VERIFIED -> ESCROW_LOCKED -> CONFIRMED | REJECTED_LOW_TRUST`) in package `relay` (`github.com/paulojauregui/acn-protocol/internal/relay`).

## 2. Target Files
1. `internal/relay/router.go`
2. `internal/relay/router_test.go`

## 3. Design-by-Contract & Architectural Invariants
- **Every exported struct, interface, constant, function, and method MUST have a Go doc comment (`// SymbolName ...`) that explicitly states `Pre-conditions:` and `Post-conditions:`.**
- **Zero emojis** in code or comments.
- Integrates `internal/identity`, `internal/reputation`, `internal/escrow`, and `internal/ledger`.

## 4. Exact Signatures & State Machine

### 4.1 `internal/relay/router.go`
- `type SessionState string`
  - `StateInitiated SessionState = "INITIATED"`
  - `StateReputationVerified SessionState = "REPUTATION_VERIFIED"`
  - `StateEscrowLocked SessionState = "ESCROW_LOCKED"`
  - `StateConfirmed SessionState = "CONFIRMED"`
  - `StateRejectedLowTrust SessionState = "REJECTED_LOW_TRUST"`
- `type BookingRequest struct`:
  - `CallerDID string` (`json:"caller_did"`) // Can be a RootDID or Rotating SubAgent DID
  - `VendorDID string` (`json:"vendor_did"`)
  - `ServiceName string` (`json:"service_name"`)
  - `BondAmountUSD float64` (`json:"bond_amount_usd"`)
  - `MinRequiredScore float64` (`json:"min_required_score"`)
  - `RequiredTrustTier identity.TrustTier` (`json:"required_trust_tier"`)
- `type NegotiationSession struct`:
  - `SessionID string` (`json:"session_id"`)
  - `AppointmentID string` (`json:"appointment_id"`)
  - `CallerDID string` (`json:"caller_did"`)
  - `CallerRootDID string` (`json:"caller_root_did"`)
  - `VendorDID string` (`json:"vendor_did"`)
  - `ServiceName string` (`json:"service_name"`)
  - `BondAmountUSD float64` (`json:"bond_amount_usd"`)
  - `CallerScore float64` (`json:"caller_score"`)
  - `CallerTier identity.TrustTier` (`json:"caller_tier"`)
  - `State SessionState` (`json:"state"`)
  - `StateHistory []SessionState` (`json:"state_history"`)
  - `LockID string` (`json:"lock_id"`)
  - `PassportID string` (`json:"passport_id"`)
  - `RejectionReason string` (`json:"rejection_reason"`)
  - `CreatedAt time.Time` (`json:"created_at"`)
- `type Router struct` with `sync.RWMutex`, holding references to `*identity.Registry`, `*reputation.Engine`, `*escrow.Vault`, `*ledger.Ledger`, and `sessions map[string]*NegotiationSession`.
- `func NewRouter(reg *identity.Registry, rep *reputation.Engine, vault *escrow.Vault, led *ledger.Ledger) *Router`
- `func (r *Router) NegotiateBooking(req BookingRequest) (*NegotiationSession, error)`
  - Begins in `StateInitiated` (`StateHistory: []SessionState{StateInitiated}`).
  - Resolves `req.CallerDID` via `r.reg.ResolveRootAgent(req.CallerDID)` so rotating sub-agents seamlessly inherit their parent Root Agent's tier, score, and bond vault while keeping `CallerDID` as the sub-agent DID for vendor interaction.
  - Evaluates caller reputation & velocity via `r.rep.Evaluate(root.RootDID)` (using `root.CompositeScore` if not yet in `rep`).
  - Checks:
    1. `callerScore >= req.MinRequiredScore`
    2. `root.TrustTier >= req.RequiredTrustTier`
    3. `req.BondAmountUSD <= root.TrustTier.MaxExposureUSD()`
    4. Velocity check: if `snap != nil && snap.VelocityCapacityUSD > 0`, checks `req.BondAmountUSD <= snap.VelocityCapacityUSD` (or if `snap != nil && snap.Metrics.StakeUSD > 0 && snap.Metrics.AccountAgeDays == 0 && snap.Metrics.SettledTxCount == 0`, rejects due to zero velocity capacity!).
  - If any trust/tier/velocity check fails, transitions to `StateRejectedLowTrust` with descriptive `RejectionReason` and returns the session (no error, or returns session so callers can inspect `State == StateRejectedLowTrust`).
  - Issues a selective-disclosure `TrustPassport` via `r.led.IssueTrustPassport` and transitions to `StateReputationVerified`.
  - Locks `req.BondAmountUSD` via `r.vault.LockBond(appointmentID, root.RootDID, req.CallerDID, req.VendorDID, req.BondAmountUSD)`. If insufficient available bond, transitions to `StateRejectedLowTrust` (`RejectionReason: "insufficient available escrow bond"`).
  - Transitions to `StateEscrowLocked` and then `StateConfirmed`.
- `func (r *Router) ListSessions() []*NegotiationSession`

### 4.2 `internal/relay/router_test.go`
- Unit tests verifying:
  - Full happy-path state machine transition `INITIATED -> REPUTATION_VERIFIED -> ESCROW_LOCKED -> CONFIRMED` using a rotating `SubAgent` DID.
  - Pre-answer rejection (`StateRejectedLowTrust`) when caller score, trust tier, or available escrow bond is below vendor requirements.
  - Concurrency safety across 32 parallel booking negotiations under `go test -race`.

## 5. Verification Command
```bash
export PATH="/home/tron/.local/go/bin:$PATH" && test -z "$(gofmt -l internal/relay)" && go vet ./internal/relay/... && go test -v -race -cover ./internal/relay/...
```
