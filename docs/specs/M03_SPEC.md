# Milestone M03 Specification: Sybil-Resistant & Whale-Resistant Reputation Engine (`internal/reputation`)

## 1. Objective
Implement the 4-component composite trust score calculator (`0.00` to `5.00`), Weight-of-Stake ($\omega$) Sybil-resistant endorsement algorithm, Time-Weighted Velocity ($\nu$) Whale-attack defense, exponential "Right to be Forgotten" slash decay, and concurrency-safe `Engine` in package `reputation` (`github.com/paulojauregui/acn-protocol/internal/reputation`).

## 2. Target Files
1. `internal/reputation/math.go`
2. `internal/reputation/engine.go`
3. `internal/reputation/engine_test.go`

## 3. Design-by-Contract & Architectural Invariants
- **Every exported struct, interface, constant, function, and method MUST have a Go doc comment (`// SymbolName ...`) that explicitly states `Pre-conditions:` and `Post-conditions:`.**
- **Zero emojis** in code or comments.
- Use only the Go 1.27.4 standard library (`errors`, `math`, `sort`, `sync`, `time`).

## 4. Exact Signatures & Mathematical Formulas

### 4.1 `internal/reputation/math.go`
- Constants:
  - `MinTrustScore = 0.0`
  - `MaxTrustScore = 5.0`
  - `DefaultInitialScore = 2.5`
  - `ProbationaryThreshold = 2.5`
  - `DefaultScaleFactor = 500.0`
  - `DefaultQualityWeight = 25.0`
  - `DefaultDecayHalfLifeDays = 30.0`
- `type Endorsement struct`:
  - `EndorserDID string` (`json:"endorser_did"`)
  - `EndorserScore float64` (`json:"endorser_score"`) // $R_i \in [0.0, 5.0]$
  - `EndorserStakeUSD float64` (`json:"endorser_stake_usd"`) // $S_i \ge 0$
  - `TransactionVolumeUSD float64` (`json:"transaction_volume_usd"`) // $V_i \ge 0$
- `type Metrics struct`:
  - `AgentDID string` (`json:"agent_did"`)
  - `FulfilledCount int` (`json:"fulfilled_count"`)
  - `NoShowCount int` (`json:"no_show_count"`)
  - `AvgCancelLeadTimeHours float64` (`json:"avg_cancel_lead_time_hours"`)
  - `PeerReviewAvg float64` (`json:"peer_review_avg"`) // `0.0` to `5.0`
  - `StakeAgeDays float64` (`json:"stake_age_days"`)
  - `StakeUSD float64` (`json:"stake_usd"`)
  - `AccountAgeDays float64` (`json:"account_age_days"`)
  - `SettledTxCount int` (`json:"settled_tx_count"`)
  - `HistoricalSlashPenalty float64` (`json:"historical_slash_penalty"`)
  - `DaysSinceLastSlash float64` (`json:"days_since_last_slash"`)
- `func ClampScore(score float64) float64`
  - Clamps `score` to `[0.0, 5.0]` (handling `math.IsNaN`).
- `func ComputeCompositeScore(m Metrics) float64`
  - Computes 4-component composite score in `[0.0, 5.0]`:
    1. **Fulfillment component** (weight `0.50`): If `FulfilledCount + NoShowCount == 0`, baseline fulfillment rate is `0.50` (`2.5` scaled to `5.0`); else `rate = float64(FulfilledCount) / float64(FulfilledCount + NoShowCount)`, scaled to `5.0 * rate`.
    2. **Cancellation Lead-Time component** (weight `0.20`): Normalized over 24 hours: `clamp(AvgCancelLeadTimeHours / 24.0, 0.0, 1.0) * 5.0` (if `AvgCancelLeadTimeHours == 0 && NoShowCount == 0 && FulfilledCount == 0`, default `2.5`).
    3. **Peer Review component** (weight `0.20`): `clamp(PeerReviewAvg, 0.0, 5.0)` (if `PeerReviewAvg == 0 && FulfilledCount == 0`, default `2.5`).
    4. **Stake Seniority component** (weight `0.10`): Normalized over 180 days: `clamp(StakeAgeDays / 180.0, 0.0, 1.0) * 5.0`.
  - Subtracts `DecayedSlashPenalty(m.HistoricalSlashPenalty, m.DaysSinceLastSlash, DefaultDecayHalfLifeDays)` and returns `ClampScore(raw - penalty)`.
- `func ApplyWeightOfStakeEndorsements(currentScore float64, endorsements []Endorsement, scaleFactor float64) float64`
  - Implements:
    $$T_{\text{new}} = \text{clamp}\left(T_{\text{old}} + \frac{\sum (R_i \times S_i \times V_i)}{N \times \text{ScaleFactor}}, 0.0, 5.0\right)$$
  - If `len(endorsements) == 0`, returns `ClampScore(currentScore)`. If `scaleFactor <= 0`, uses `DefaultScaleFactor`. Ignores negative $R_i, S_i, V_i$.
- `func ComputeVelocityCapacity(stakeUSD, accountAgeDays float64, settledTxCount int, qualityWeight float64) float64`
  - Implements:
    $$\text{VelocityCapacity} = (\text{StakeUSD} \times \text{AccountAgeDays}) + (\text{SettledTxCount} \times \text{QualityWeight})$$
  - Clamps negative inputs to `0`. If `qualityWeight <= 0`, uses `DefaultQualityWeight`.
- `func DecayedSlashPenalty(initialPenalty, daysElapsed, halfLifeDays float64) float64`
  - Implements exponential "Right to be Forgotten" decay:
    $$\text{Penalty}(t) = \text{initialPenalty} \times 2^{-t / \text{halfLifeDays}}$$
- `func IsProbationary(score float64) bool`
  - Returns `score < ProbationaryThreshold` (`2.5`).

### 4.2 `internal/reputation/engine.go`
- `type Snapshot struct`:
  - `AgentDID string` (`json:"agent_did"`)
  - `CompositeScore float64` (`json:"composite_score"`)
  - `FulfillmentRate float64` (`json:"fulfillment_rate"`)
  - `VelocityCapacityUSD float64` (`json:"velocity_capacity_usd"`)
  - `Probationary bool` (`json:"probationary"`)
  - `Metrics Metrics` (`json:"metrics"`)
  - `UpdatedAt time.Time` (`json:"updated_at"`)
- `type Engine struct` with `sync.RWMutex`.
- `func NewEngine() *Engine`
- `func (e *Engine) UpsertMetrics(m Metrics) (*Snapshot, error)`
- `func (e *Engine) RecordSettlement(agentDID string, fulfilled bool, peerRating float64, cancelLeadHours float64) (*Snapshot, error)`
  - If `fulfilled == true`: increments `FulfilledCount`, increments `SettledTxCount`, updates running average `PeerReviewAvg` and `AvgCancelLeadTimeHours`.
  - If `fulfilled == false` (No-Show / Slash): increments `NoShowCount`, adds `1.25` to `HistoricalSlashPenalty`, resets `DaysSinceLastSlash = 0`.
- `func (e *Engine) ApplyEndorsements(agentDID string, endorsements []Endorsement) (*Snapshot, error)`
- `func (e *Engine) AdvanceTimeDays(agentDID string, days float64) (*Snapshot, error)`
  - Advances `AccountAgeDays`, `StakeAgeDays` (if `StakeUSD > 0`), and `DaysSinceLastSlash` by `days`, recomputing the score (demonstrating "Right to be Forgotten" recovery).
- `func (e *Engine) Evaluate(agentDID string) (*Snapshot, error)`
- `func (e *Engine) CheckVelocityAllowed(agentDID string, requestedExposureUSD float64) (bool, float64, error)`
  - Returns `(requestedExposureUSD <= snap.VelocityCapacityUSD, snap.VelocityCapacityUSD, nil)`.

### 4.3 `internal/reputation/engine_test.go`
- Unit tests proving:
  - 4-component composite score math.
  - Zero-stake Sybil wash-trading ($S_i = 0$ or $V_i = 0$) yields `0.0` score increase across 1,000 endorsements, whereas staked high-reputation endorsements raise the score.
  - Whale attack defense: `$1,000,000` stake with `AccountAgeDays = 0` and `SettledTxCount = 0` yields `VelocityCapacity = 0`, blocking high-exposure bookings until age/settled history accrues.
  - Exponential slash decay (`AdvanceTimeDays`) recovers an agent out of `Probationary` status over time.
  - Race safety across concurrent goroutines (`go test -race`).

## 5. Verification Command
```bash
export PATH="/home/tron/.local/go/bin:$PATH" && test -z "$(gofmt -l internal/reputation)" && go vet ./internal/reputation/... && go test -v -race -cover ./internal/reputation/...
```
