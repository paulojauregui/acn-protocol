# Milestone M02 Specification: Sovereign Identity, DIDs & Rotating Sub-Agents (`internal/identity`)

## 1. Objective
Implement the Sovereign Decentralized Identifier (`did:acn:*`) cryptographic primitives, rotating unlinkable `SubAgent` (`did:acn:sub:*`) address generator, 4-Tier Trust Ladder calculator, and concurrency-safe `Registry` in package `identity` (`github.com/paulojauregui/acn-protocol/internal/identity`).

## 2. Target Files
1. `internal/identity/did.go`
2. `internal/identity/registry.go`
3. `internal/identity/registry_test.go`

## 3. Design-by-Contract & Architectural Invariants
- **Every exported struct, interface, constant block, function, and method MUST have a Go doc comment (`// SymbolName ...`) that explicitly states `Pre-conditions:` and `Post-conditions:`.**
- **Zero emojis** in code or comments.
- Use only the Go 1.27.4 standard library (`crypto/ed25519`, `crypto/rand`, `crypto/sha256`, `encoding/hex`, `errors`, `fmt`, `sort`, `strings`, `sync`, `time`).

## 4. Exact Signatures & Behavior

### 4.1 `internal/identity/did.go`
- `type TrustTier int`
  - `Tier0Ephemeral TrustTier = 0` (`<= $5.00` max exposure)
  - `Tier1Economic TrustTier = 1` (`<= $150.00` max exposure; requires `bondUSD >= 50.0 || settledTxCount >= 100`)
  - `Tier2Reputational TrustTier = 2` (`<= $2000.00` max exposure; requires `bondUSD >= 50.0 && cumulativeSpendUSD >= 500.0 && settledTxCount >= 20`)
  - `Tier3Sovereign TrustTier = 3` (`<= $1000000.00` unrestricted exposure; requires `popVerified && compositeScore >= 4.5 && bondUSD >= 50.0 && cumulativeSpendUSD >= 500.0 && settledTxCount >= 20`)
- `func (t TrustTier) String() string`
  - Returns `"TIER_0_EPHEMERAL"`, `"TIER_1_ECONOMIC"`, `"TIER_2_REPUTATIONAL"`, or `"TIER_3_SOVEREIGN"`.
- `func (t TrustTier) MaxExposureUSD() float64`
  - Returns `5.0`, `150.0`, `2000.0`, or `1000000.0` respectively.
- `type KeyPair struct { PublicKey ed25519.PublicKey; PrivateKey ed25519.PrivateKey }`
- `func GenerateKeyPair() (*KeyPair, error)`
- `func DeriveRootDID(pub ed25519.PublicKey) (string, error)`
  - Validates `len(pub) == ed25519.PublicKeySize`. Computes `sha256.Sum256(pub)` and returns `"did:acn:" + hex.EncodeToString(sum[:16])`.
- `func (kp *KeyPair) Sign(message []byte) ([]byte, error)`
  - Validates `kp != nil && len(kp.PrivateKey) == ed25519.PrivateKeySize` and `len(message) > 0`. Returns `ed25519.Sign(kp.PrivateKey, message)`.
- `func VerifySignature(pub ed25519.PublicKey, message, sig []byte) bool`
  - Validates `len(pub) == ed25519.PublicKeySize && len(sig) == ed25519.SignatureSize && len(message) > 0` and calls `ed25519.Verify(pub, message, sig)`.
- `type RootAgent struct`:
  - `RootDID string` (`json:"root_did"`)
  - `DisplayName string` (`json:"display_name"`)
  - `PublicKey []byte` (`json:"public_key"`)
  - `PoPVerified bool` (`json:"pop_verified"`)
  - `TEEAttested bool` (`json:"tee_attested"`)
  - `BondUSD float64` (`json:"bond_usd"`)
  - `CumulativeSpendUSD float64` (`json:"cumulative_spend_usd"`)
  - `SettledTxCount int` (`json:"settled_tx_count"`)
  - `SlashCount int` (`json:"slash_count"`)
  - `CompositeScore float64` (`json:"composite_score"`)
  - `TrustTier TrustTier` (`json:"trust_tier"`)
  - `IsVendor bool` (`json:"is_vendor"`)
  - `CreatedAt time.Time` (`json:"created_at"`)
- `type SubAgent struct`:
  - `SubDID string` (`json:"sub_did"`)
  - `ParentRootDID string` (`json:"parent_root_did"`)
  - `Purpose string` (`json:"purpose"`)
  - `EphemeralPubKey []byte` (`json:"ephemeral_pub_key"`)
  - `RootSignature []byte` (`json:"root_signature"`)
  - `CreatedAt time.Time` (`json:"created_at"`)
  - `ExpiresAt time.Time` (`json:"expires_at"`)
- `func CalculateTrustTier(bondUSD, cumulativeSpendUSD float64, settledTxCount int, popVerified bool, compositeScore float64) TrustTier`

### 4.2 `internal/identity/registry.go`
- `type Registry struct` with `sync.RWMutex`.
- `func NewRegistry() *Registry`
- `func (r *Registry) RegisterRootAgent(displayName string, pub ed25519.PublicKey, popVerified, teeAttested, isVendor bool) (*RootAgent, error)`
  - If `len(pub) == 0`, generates a new Ed25519 public key automatically so callers can pass `nil` or an explicit public key.
  - Initializes `CompositeScore: 2.50`, `TrustTier: Tier0Ephemeral`, `CreatedAt: time.Now().UTC()`.
- `func (r *Registry) SpawnSubAgent(rootDID, purpose string, rootPrivKey ed25519.PrivateKey, ttl time.Duration) (*SubAgent, error)`
  - Generates an ephemeral Ed25519 keypair for the sub-agent, derives `did:acn:sub:<16-byte-hex>`, signs the sub-DID payload with `rootPrivKey` (if `len(rootPrivKey) == ed25519.PrivateKeySize`, verifies against `root.PublicKey`; if `len(rootPrivKey) == 0`, produces a deterministic SHA-256 binding digest), and links `SubDID -> RootDID`.
- `func (r *Registry) ResolveRootAgent(did string) (*RootAgent, error)`
  - Resolves either a `did:acn:<hex>` RootDID or a `did:acn:sub:<hex>` SubDID to a deep copy of its `*RootAgent`.
- `func (r *Registry) UpdateEconomics(did string, bondDeltaUSD, spendDeltaUSD float64, settledTxDelta int, newScore float64) (*RootAgent, error)`
  - Resolves `did` (Root or Sub) to its underlying `RootAgent`, updates `BondUSD` (clamped `>= 0`), `CumulativeSpendUSD` (`>= 0`), `SettledTxCount` (`>= 0`), and `CompositeScore` (if `newScore >= 0`, clamped to `[0.0, 5.0]`), recomputes `TrustTier` via `CalculateTrustTier`, and returns a copy.
- `func (r *Registry) RecordSlash(did string, bondPenaltyUSD float64, newScore float64) (*RootAgent, error)`
  - Resolves `did` (crucially supporting `SubAgent` DIDs so any sub-agent infraction rolls up directly to the parent `RootAgent`), increments `SlashCount`, deducts `bondPenaltyUSD` from `BondUSD` (clamped `>= 0`), updates `CompositeScore` to `newScore`, recomputes `TrustTier`, and returns a copy.
- `func (r *Registry) ListRootAgents() []*RootAgent` (sorted deterministically by `RootDID`)
- `func (r *Registry) ListSubAgents(rootDID string) []*SubAgent` (if `rootDID == ""`, returns all sub-agents sorted by `SubDID`)

### 4.3 `internal/identity/registry_test.go`
- Unit tests covering:
  - Ed25519 keypair generation, `DeriveRootDID`, `Sign`, and `VerifySignature`.
  - All 4 tiers of `CalculateTrustTier` (`Tier0Ephemeral`, `Tier1Economic` via bond and via 100 settled tx, `Tier2Reputational`, and `Tier3Sovereign`).
  - `SpawnSubAgent` unlinkability and `RecordSlash` rollup from `did:acn:sub:*` to parent `RootAgent` (including demotion from Tier 3/2 down to Tier 0/1 when bond/score is slashed).
  - Concurrent reads/writes across 32 goroutines verified under `go test -race`.

## 5. Verification Command
```bash
export PATH="/home/tron/.local/go/bin:$PATH" && test -z "$(gofmt -l internal/identity)" && go vet ./internal/identity/... && go test -v -race -cover ./internal/identity/...
```
