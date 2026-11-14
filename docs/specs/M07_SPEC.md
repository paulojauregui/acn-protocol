# Milestone M07 Specification: Governance Constitution, Reputation-Weighted Voting & DGX Spark Audit Jury (`internal/governance`)

## 1. Objective
Implement the Protocol Constitution parameters, Reputation-Weighted Governance Voting ($V_i = \ln(1 + S_i) \times R_i$) with validator veto checks, and the Hybrid Deterministic + DGX Spark AI Audit Jury (`ArbitrateDispute`) in package `governance` (`github.com/paulojauregui/acn-protocol/internal/governance`).

## 2. Target Files
1. `internal/governance/voting.go`
2. `internal/governance/jury.go`
3. `internal/governance/governance_test.go`

## 3. Design-by-Contract & Architectural Invariants
- **Every exported struct, interface, constant, function, and method MUST have a Go doc comment (`// SymbolName ...`) that explicitly states `Pre-conditions:` and `Post-conditions:`.**
- **Zero emojis** in code or comments.
- Use only the Go 1.27.4 standard library (`bytes`, `context`, `encoding/json`, `errors`, `fmt`, `math`, `net/http`, `os`, `sort`, `strings`, `sync`, `time`).

## 4. Exact Signatures & Behavior

### 4.1 `internal/governance/voting.go`
- `type Constitution struct`:
  - `NetworkIntegrityFeeRate float64` (`json:"network_integrity_fee_rate"`) // `0.005`
  - `VendorSlashShare float64` (`json:"vendor_slash_share"`) // `0.70`
  - `NetworkSlashShare float64` (`json:"network_slash_share"`) // `0.30`
  - `ProbationaryScoreThreshold float64` (`json:"probationary_score_threshold"`) // `2.50`
  - `NonceTTLSeconds int` (`json:"nonce_ttl_seconds"`) // `60`
- `func DefaultConstitution() Constitution`
- `type ProposalStatus string`
  - `ProposalActive ProposalStatus = "ACTIVE"`
  - `ProposalPassed ProposalStatus = "PASSED"`
  - `ProposalRejected ProposalStatus = "REJECTED"`
  - `ProposalVetoed ProposalStatus = "VETOED_BY_VALIDATORS"`
- `type VoteReceipt struct`:
  - `ProposalID string` (`json:"proposal_id"`)
  - `VoterDID string` (`json:"voter_did"`)
  - `StakeUSD float64` (`json:"stake_usd"`)
  - `ReputationScore float64` (`json:"reputation_score"`)
  - `VoteWeight float64` (`json:"vote_weight"`)
  - `Support bool` (`json:"support"`)
  - `ValidatorVeto bool` (`json:"validator_veto"`)
- `type Proposal struct`:
  - `ProposalID string` (`json:"proposal_id"`)
  - `Title string` (`json:"title"`)
  - `Description string` (`json:"description"`)
  - `ParameterKey string` (`json:"parameter_key"`)
  - `ProposedValue string` (`json:"proposed_value"`)
  - `YesWeight float64` (`json:"yes_weight"`)
  - `NoWeight float64` (`json:"no_weight"`)
  - `ValidatorVetoed bool` (`json:"validator_vetoed"`)
  - `Status ProposalStatus` (`json:"status"`)
  - `CreatedAt time.Time` (`json:"created_at"`)
- `func ComputeVoteWeight(stakeUSD, reputationScore float64) float64`
  - Implements:
    $$V_i = \ln(1 + S_i) \times R_i$$
  - Uses `math.Log1p(math.Max(0, stakeUSD)) * math.Max(0, math.Min(5.0, reputationScore))`.
- `type Engine struct` with `sync.RWMutex`, storing `constitution Constitution`, `proposals map[string]*Proposal`, `votes map[string]map[string]VoteReceipt`.
- `func NewEngine() *Engine`
- `func (e *Engine) GetConstitution() Constitution`
- `func (e *Engine) CreateProposal(id, title, description, paramKey, proposedVal string) (*Proposal, error)`
- `func (e *Engine) CastVote(proposalID, voterDID string, stakeUSD, reputationScore float64, support, isValidatorVeto bool) (*VoteReceipt, *Proposal, error)`
  - Computes `weight = ComputeVoteWeight(stakeUSD, reputationScore)`.
  - If `isValidatorVeto == true`, marks `Proposal.ValidatorVetoed = true` and `Proposal.Status = ProposalVetoed`.
  - Otherwise updates `YesWeight` or `NoWeight` and updates status (`ProposalPassed` if `YesWeight > NoWeight && !ValidatorVetoed`).
- `func (e *Engine) ListProposals() []*Proposal`

### 4.2 `internal/governance/jury.go`
- Constants:
  - `DefaultSparkBaseURL = "http://happy.tail749110.ts.net:8000/v1/chat/completions"`
  - `DefaultSparkModel = "nvidia/Qwen3.8-Flash-Next-FP4-Local"`
  - `DefaultJuryTimeout = 1800 * time.Millisecond` (`< 2.0s` strict bound)
- `type DisputeCase struct`:
  - `CaseID string` (`json:"case_id"`)
  - `AppointmentID string` (`json:"appointment_id"`)
  - `UserDID string` (`json:"user_did"`)
  - `VendorDID string` (`json:"vendor_did"`)
  - `UserClaimPresent bool` (`json:"user_claim_present"`)
  - `VendorClaimPresent bool` (`json:"vendor_claim_present"`)
  - `StripePOSReceipt bool` (`json:"stripe_pos_receipt"`)
  - `LocalBeaconVerified bool` (`json:"local_beacon_verified"`)
  - `UserReputation float64` (`json:"user_reputation"`)
  - `VendorConflictCount int` (`json:"vendor_conflict_count"`)
- `type JuryVerdict struct`:
  - `CaseID string` (`json:"case_id"`)
  - `AppointmentID string` (`json:"appointment_id"`)
  - `Winner string` (`json:"winner"`) // `"USER"` or `"VENDOR"`
  - `SlashUser bool` (`json:"slash_user"`)
  - `SlashVendor bool` (`json:"slash_vendor"`)
  - `EngineUsed string` (`json:"engine_used"`) // `"DGX_SPARK_LLM"` or `"DETERMINISTIC_MULTI_FACTOR"`
  - `Rationale string` (`json:"rationale"`)
  - `DecidedAt time.Time` (`json:"decided_at"`)
- `type Jury struct` with `sync.RWMutex`, `sparkURL string`, `model string`, `enableRemoteLLM bool`, `httpClient *http.Client`, `verdicts map[string]*JuryVerdict`.
- `func NewJury(sparkURL string, enableRemoteLLM bool) *Jury`
  - Configures `httpClient` with `Timeout: DefaultJuryTimeout` (`< 2.0s`).
- `func (j *Jury) ArbitrateDispute(ctx context.Context, c DisputeCase) (*JuryVerdict, error)`
  - First evaluates deterministic multi-factor truth:
    - If `c.StripePOSReceipt && c.LocalBeaconVerified`: hardware beacon + POS receipt prove user presence (`Winner: "USER"`, `SlashUser: false`, `SlashVendor: true`).
    - Else if `(c.StripePOSReceipt || c.LocalBeaconVerified) && c.VendorConflictCount >= 2`: (`Winner: "USER"`, `SlashUser: false`, `SlashVendor: true`).
    - Else if `!c.StripePOSReceipt && !c.LocalBeaconVerified && !c.VendorClaimPresent`: (`Winner: "VENDOR"`, `SlashUser: true`, `SlashVendor: false`).
  - If `j.enableRemoteLLM` is true and `j.sparkURL != ""`, performs a `< 2.0s` bounded OpenAI chat completion request to `j.sparkURL` to enrich `Rationale` and set `EngineUsed = "DGX_SPARK_LLM"`; if the HTTP call fails or times out, seamlessly falls back to `"DETERMINISTIC_MULTI_FACTOR"` without returning an error.

### 4.3 `internal/governance/governance_test.go`
- Unit tests verifying:
  - `ComputeVoteWeight` ($V_i = \ln(1+S_i) \times R_i$) where zero-stake ($S_i=0$) or zero-reputation ($R_i=0$) produces `0.0` voting power.
  - Validator veto overriding majority vote weight.
  - `ArbitrateDispute` both in deterministic mode and against a mocked `httptest.Server` (testing both healthy `< 2.0s` LLM response and instant timeout/500 fallback).
  - Race safety under `go test -race`.

## 5. Verification Command
```bash
export PATH="/home/tron/.local/go/bin:$PATH" && test -z "$(gofmt -l internal/governance)" && go vet ./internal/governance/... && go test -v -race -cover ./internal/governance/...
```
