# Milestone M05 Specification: Double-Blind Accountability Oracle & 60s Nonce Handshake (`internal/oracle`)

## 1. Objective
Implement time-bound 60-second cryptographic challenge nonces (`IssueChallengeNonce`) with replay protection and the Multi-Factor Double-Blind Mutual Attestation Oracle (`SettleAppointment`) in package `oracle` (`github.com/paulojauregui/acn-protocol/internal/oracle`).

## 2. Target Files
1. `internal/oracle/handshake.go`
2. `internal/oracle/oracle.go`
3. `internal/oracle/oracle_test.go`

## 3. Design-by-Contract & Architectural Invariants
- **Every exported struct, interface, constant, function, and method MUST have a Go doc comment (`// SymbolName ...`) that explicitly states `Pre-conditions:` and `Post-conditions:`.**
- **Zero emojis** in code or comments.
- Keep implementation concise and clean using the Go 1.27.4 standard library (`crypto/hmac`, `crypto/rand`, `crypto/sha256`, `encoding/hex`, `errors`, `fmt`, `sync`, `time`).

## 4. Exact Signatures & State Machine

### 4.1 `internal/oracle/handshake.go`
- Constants:
  - `DefaultNonceTTL = 60 * time.Second`
  - `VendorHonestReporterRewardUSD = 0.25`
  - `VendorConflictSlashThreshold = 3`
- `type ChallengeNonce struct`:
  - `Nonce string` (`json:"nonce"`)
  - `AppointmentID string` (`json:"appointment_id"`)
  - `UserDID string` (`json:"user_did"`)
  - `VendorDID string` (`json:"vendor_did"`)
  - `IssuedAt time.Time` (`json:"issued_at"`)
  - `ExpiresAt time.Time` (`json:"expires_at"`)
  - `Consumed bool` (`json:"consumed"`)
- `type SignedPresenceToken struct`:
  - `AppointmentID string` (`json:"appointment_id"`)
  - `Nonce string` (`json:"nonce"`)
  - `SignerDID string` (`json:"signer_did"`)
  - `Present bool` (`json:"present"`)
  - `Signature string` (`json:"signature"`)
  - `Timestamp time.Time` (`json:"timestamp"`)
- `func ComputePresenceSignature(secret string, appointmentID, nonce, signerDID string, present bool) string`
  - Computes deterministic HMAC-SHA256 hex signature over `fmt.Sprintf("%s|%s|%s|%t", appointmentID, nonce, signerDID, present)`.
- `func VerifyPresenceSignature(secret string, token SignedPresenceToken) bool`

### 4.2 `internal/oracle/oracle.go`
- `type SettlementStatus string`
  - `StatusSuccess SettlementStatus = "STATUS_SUCCESS"`
  - `StatusSlashed SettlementStatus = "STATUS_SLASHED"`
  - `StatusDisputed SettlementStatus = "STATUS_DISPUTED"`
- `type SettlementInput struct`:
  - `AppointmentID string` (`json:"appointment_id"`)
  - `LockID string` (`json:"lock_id"`)
  - `Nonce string` (`json:"nonce"`)
  - `UserRootDID string` (`json:"user_root_did"`)
  - `UserDID string` (`json:"user_did"`)
  - `VendorDID string` (`json:"vendor_did"`)
  - `UserPresent bool` (`json:"user_present"`)
  - `VendorPresent bool` (`json:"vendor_present"`)
  - `StripePOSReceipt bool` (`json:"stripe_pos_receipt"`)
  - `LocalBeaconVerified bool` (`json:"local_beacon_verified"`)
  - `PeerRating float64` (`json:"peer_rating"`)
- `type SettlementOutcome struct`:
  - `AppointmentID string` (`json:"appointment_id"`)
  - `LockID string` (`json:"lock_id"`)
  - `Nonce string` (`json:"nonce"`)
  - `Status SettlementStatus` (`json:"status"`)
  - `UserSlashed bool` (`json:"user_slashed"`)
  - `VendorSlashed bool` (`json:"vendor_slashed"`)
  - `HonestReporterRewardUSD float64` (`json:"honest_reporter_reward_usd"`)
  - `VendorConflictCount int` (`json:"vendor_conflict_count"`)
  - `ResolutionReason string` (`json:"resolution_reason"`)
  - `SettledAt time.Time` (`json:"settled_at"`)
- `type Oracle struct` with `sync.RWMutex`, tracking `nonces map[string]*ChallengeNonce`, `outcomes map[string]*SettlementOutcome`, `vendorConflicts map[string]int`, `nowFunc func() time.Time`.
- `func NewOracle() *Oracle`
- `func (o *Oracle) IssueChallengeNonce(appointmentID, userDID, vendorDID string) (*ChallengeNonce, error)`
  - Generates a 16-byte random hex nonce with `ExpiresAt = now.Add(DefaultNonceTTL)`.
- `func (o *Oracle) ExpireNonceForTest(nonce string)`
  - Helper to set `ExpiresAt` in the past for deterministic unit tests.
- `func (o *Oracle) SettleAppointment(in SettlementInput) (*SettlementOutcome, error)`
  - Validates that `in.Nonce` exists, matches `in.AppointmentID`, is not already `Consumed` (replay protection), and has not expired (`now <= ExpiresAt`). Marks `Consumed = true`.
  - **Case 1 (`in.UserPresent && in.VendorPresent`)**:
    - Both attest arrival -> `Status: StatusSuccess`, `HonestReporterRewardUSD: VendorHonestReporterRewardUSD`.
  - **Case 2 (`!in.UserPresent && !in.VendorPresent`)**:
    - Mutual/confirmed user absence -> `Status: StatusSlashed`, `UserSlashed: true`.
  - **Case 3 (`in.UserPresent != in.VendorPresent`)**:
    - Conflicting claims -> `Status: StatusDisputed`. Increments `vendorConflicts[in.VendorDID]`.
    - Multi-factor check: if `in.UserPresent && !in.VendorPresent` AND `(in.StripePOSReceipt && in.LocalBeaconVerified)`:
      - Third-party hardware/POS proof confirms the user was physically present and the vendor fabricated or misreported a No-Show! `UserSlashed = false`, `VendorSlashed = true` (especially when conflict count >= 1 with hard proof or >= `VendorConflictSlashThreshold`).
    - Conversely, if `!in.StripePOSReceipt && !in.LocalBeaconVerified`:
      - Uncorroborated user claim against vendor No-Show report -> `UserSlashed = true`, `VendorSlashed = false`.
- `func (o *Oracle) GetOutcome(appointmentID string) (*SettlementOutcome, error)`
- `func (o *Oracle) ListOutcomes() []*SettlementOutcome`
- `func (o *Oracle) GetVendorConflictCount(vendorDID string) int`

### 4.3 `internal/oracle/oracle_test.go`
- Unit tests verifying:
  - 60s nonce issuance, replay rejection (second settlement with same nonce fails), and expiration rejection (`ExpireNonceForTest`).
  - `STATUS_SUCCESS` when both user and vendor attest arrival.
  - `STATUS_SLASHED` when user fails to show.
  - `STATUS_DISPUTED` with multi-factor evidence (`StripePOSReceipt && LocalBeaconVerified`) protecting the honest user and flagging `VendorSlashed = true`.
  - Concurrency safety under `go test -race`.

## 5. Verification Command
```bash
export PATH="/home/tron/.local/go/bin:$PATH" && test -z "$(gofmt -l internal/oracle)" && go vet ./internal/oracle/... && go test -v -race -cover ./internal/oracle/...
```
