# Milestone M06 Specification: Merkle Tree Ledger Batcher & Privacy-Preserving Trust Passports (`internal/ledger`)

## 1. Objective
Implement the append-only SHA-256 Merkle Tree ledger (`ComputeMerkleRoot`, `GenerateInclusionProof`, `VerifyInclusionProof`) and the Privacy-Preserving Selective-Disclosure `TrustPassport` issuer and verifier in package `ledger` (`github.com/paulojauregui/acn-protocol/internal/ledger`).

## 2. Target Files
1. `internal/ledger/merkle.go`
2. `internal/ledger/passport.go`
3. `internal/ledger/ledger_test.go`

## 3. Design-by-Contract & Architectural Invariants
- **Every exported struct, interface, constant, function, and method MUST have a Go doc comment (`// SymbolName ...`) that explicitly states `Pre-conditions:` and `Post-conditions:`.**
- **Zero emojis** in code or comments.
- Use only the Go 1.27.4 standard library (`crypto/ed25519`, `crypto/rand`, `crypto/sha256`, `encoding/hex`, `encoding/json`, `errors`, `fmt`, `sync`, `time`).

## 4. Exact Signatures & Cryptographic Construction

### 4.1 `internal/ledger/merkle.go`
- `type InteractionRecord struct`:
  - `RecordID string` (`json:"record_id"`)
  - `AppointmentID string` (`json:"appointment_id"`)
  - `UserRootDID string` (`json:"user_root_did"`)
  - `SubAgentDID string` (`json:"sub_agent_did"`)
  - `VendorDID string` (`json:"vendor_did"`)
  - `Status string` (`json:"status"`)
  - `AmountUSD float64` (`json:"amount_usd"`)
  - `Timestamp time.Time` (`json:"timestamp"`)
  - `LeafHash string` (`json:"leaf_hash"`)
- `type ProofStep struct`:
  - `SiblingHash string` (`json:"sibling_hash"`)
  - `IsLeft bool` (`json:"is_left"`) // true if sibling is on the left (`H(sibling || current)`)
- `type InclusionProof struct`:
  - `LeafHash string` (`json:"leaf_hash"`)
  - `LeafIndex int` (`json:"leaf_index"`)
  - `MerkleRoot string` (`json:"merkle_root"`)
  - `Steps []ProofStep` (`json:"steps"`)
- `func HashLeaf(data string) string`
  - Returns SHA-256 hex digest of `"leaf:" + data`.
- `func HashNodes(leftHex, rightHex string) string`
  - Returns SHA-256 hex digest of `"node:" + leftHex + ":" + rightHex`.
- `func ComputeMerkleRoot(leafHashes []string) string`
  - If `len(leafHashes) == 0`, returns SHA-256 hex of `"empty"`.
  - At each tree level, if the number of nodes is odd, duplicates the last node (`level = append(level, level[len(level)-1])`), then hashes adjacent pairs `HashNodes(level[i], level[i+1])` until 1 root hash remains.
- `func GenerateInclusionProof(leafHashes []string, targetIndex int) (*InclusionProof, error)`
  - Validates `0 <= targetIndex < len(leafHashes)` and constructs the `[]ProofStep` path from `leafHashes[targetIndex]` up to `ComputeMerkleRoot(leafHashes)`.
- `func VerifyInclusionProof(proof *InclusionProof) bool`
  - Recomputes the root from `proof.LeafHash` and `proof.Steps` using `HashNodes` and returns `computedRoot == proof.MerkleRoot`.
- `type Ledger struct` with `sync.RWMutex`, storing `records []InteractionRecord`, `leafHashes []string`, `enclavePub ed25519.PublicKey`, `enclavePriv ed25519.PrivateKey`.
- `func NewLedger() *Ledger`
- `func (l *Ledger) AppendRecord(rec InteractionRecord) (InteractionRecord, string, error)`
  - Computes `rec.LeafHash = HashLeaf(fmt.Sprintf("%s|%s|%s|%s|%.2f", rec.AppointmentID, rec.UserRootDID, rec.VendorDID, rec.Status, rec.AmountUSD))`, appends to `records`, and returns `(rec, l.CurrentMerkleRoot(), nil)`.
- `func (l *Ledger) CurrentMerkleRoot() string`
- `func (l *Ledger) GetInclusionProof(index int) (*InclusionProof, error)`
- `func (l *Ledger) ListRecords() []InteractionRecord`

### 4.2 `internal/ledger/passport.go`
- `type TrustPassport struct`:
  - `PassportID string` (`json:"passport_id"`)
  - `SubjectDID string` (`json:"subject_did"`) // Can be a Rotating Sub-Agent DID so root identity remains private
  - `MerkleRoot string` (`json:"merkle_root"`)
  - `MinFulfillmentRate float64` (`json:"min_fulfillment_rate"`)
  - `MinSettledBookings int` (`json:"min_settled_bookings"`)
  - `MinTrustTier int` (`json:"min_trust_tier"`)
  - `CompositeScoreBucket string` (`json:"composite_score_bucket"`) // e.g. ">=4.5" or ">=3.5"
  - `CommitmentSaltHash string` (`json:"commitment_salt_hash"`)
  - `EnclaveAttestationSig string` (`json:"enclave_attestation_sig"`)
  - `IssuedAt time.Time` (`json:"issued_at"`)
  - `ExpiresAt time.Time` (`json:"expires_at"`)
- `func (l *Ledger) IssueTrustPassport(subjectDID string, actualFulfillmentRate float64, actualSettledBookings int, actualTrustTier int, actualScore float64, reqMinFulfillment float64, reqMinBookings int, reqMinTier int) (*TrustPassport, error)`
  - Validates that the agent actually meets the requested predicates (`actualFulfillmentRate >= reqMinFulfillment`, `actualSettledBookings >= reqMinBookings`, `actualTrustTier >= reqMinTier`). If not, returns an error.
  - Signs the canonical predicate payload anchored to `l.CurrentMerkleRoot()` using `l.enclavePriv` (`ed25519.Sign`) without including any historical vendor names or appointment timestamps.
- `func (l *Ledger) VerifyTrustPassport(p *TrustPassport) bool`
  - Verifies `p != nil`, `time.Now().Before(p.ExpiresAt)`, and `ed25519.Verify(l.enclavePub, canonicalPayload(p), sigBytes)`.

### 4.3 `internal/ledger/ledger_test.go`
- Unit tests verifying:
  - `ComputeMerkleRoot`, `GenerateInclusionProof`, and `VerifyInclusionProof` across 1, 2, 5, and 16 leaves, plus tamper rejection when a proof step or leaf hash is modified.
  - `IssueTrustPassport` and `VerifyTrustPassport` selective-disclosure predicates (succeeds when agent satisfies predicates, rejects when agent fails predicate or signature is tampered).
  - Concurrency safety under `go test -race`.

## 5. Verification Command
```bash
export PATH="/home/tron/.local/go/bin:$PATH" && test -z "$(gofmt -l internal/ledger)" && go vet ./internal/ledger/... && go test -v -race -cover ./internal/ledger/...
```
