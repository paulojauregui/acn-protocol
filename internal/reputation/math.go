// Package reputation implements the ACN Sybil-resistant and whale-resistant
// trust layer: the 4-component composite score, Weight-of-Stake endorsements,
// Time-Weighted Velocity capacity and exponential slash decay.
package reputation

import "math"

// Reputation tuning constants shared by the math helpers and the Engine.
//
// Pre-conditions: none (compile-time constants, never mutated at runtime).
// Post-conditions: every score produced by this package lies inside
// [MinTrustScore, MaxTrustScore] and every default is strictly positive.
const (
	// MinTrustScore and MaxTrustScore bound every composite score.
	MinTrustScore = 0.0
	MaxTrustScore = 5.0
	// DefaultInitialScore is the neutral score of an agent with no history.
	DefaultInitialScore = 2.5
	// ProbationaryThreshold is the gate below which an agent is restricted.
	ProbationaryThreshold = 2.5
	// DefaultScaleFactor normalizes the Weight-of-Stake endorsement sum.
	DefaultScaleFactor = 500.0
	// DefaultQualityWeight prices each settled transaction in VelocityCapacity.
	DefaultQualityWeight = 25.0
	// DefaultDecayHalfLifeDays is the slash-penalty decay half-life.
	DefaultDecayHalfLifeDays = 30.0
)

// Component weights of the 4-part composite score (they sum to exactly 1.0).
//
// Pre-conditions: none (compile-time constants).
// Post-conditions: weightFulfillment+weightCancel+weightPeer+weightSeniority
// equals 1.0, so an agent with no history scores exactly DefaultInitialScore.
const (
	weightFulfillment = 0.50
	weightCancel      = 0.20
	weightPeer        = 0.20
	weightSeniority   = 0.10
)

// Normalization horizons of the cancel lead-time and seniority signals.
//
// Pre-conditions: none (compile-time constants).
// Post-conditions: cancelFullHours yields a full-marks lead-time component and
// seniorityFullDays a full-marks stake seniority component.
const (
	cancelFullHours   = 24.0
	seniorityFullDays = 180.0
)

// SlashPenaltyPerNoShow is the penalty added by a single no-show settlement.
//
// Pre-conditions: none (compile-time constant).
// Post-conditions: RecordSettlement adds exactly this much to
// Metrics.HistoricalSlashPenalty per no-show before decay is applied.
const SlashPenaltyPerNoShow = 1.25

// Endorsement is one Weight-of-Stake vote cast by an endorser for an agent.
//
// Pre-conditions: EndorserDID identifies a registered agent; negative R_i, S_i
// or V_i values are accepted but treated as zero by the math helpers.
// Post-conditions: the struct is pure JSON round-trippable data with no
// pointers, so it is safe to share across goroutines.
type Endorsement struct {
	EndorserDID          string  `json:"endorser_did"`
	EndorserScore        float64 `json:"endorser_score"`
	EndorserStakeUSD     float64 `json:"endorser_stake_usd"`
	TransactionVolumeUSD float64 `json:"transaction_volume_usd"`
}

// Metrics is the observable behavioral and economic history of one agent.
//
// Pre-conditions: AgentDID is non-empty; counters are >= 0; PeerReviewAvg is a
// value in [0.0, 5.0] (out-of-range input is clamped before use).
// Post-conditions: the struct is pure data, so a copy taken under the Engine
// read lock can be serialized or mutated by the caller without racing.
type Metrics struct {
	AgentDID               string  `json:"agent_did"`
	FulfilledCount         int     `json:"fulfilled_count"`
	NoShowCount            int     `json:"no_show_count"`
	AvgCancelLeadTimeHours float64 `json:"avg_cancel_lead_time_hours"`
	PeerReviewAvg          float64 `json:"peer_review_avg"`
	StakeAgeDays           float64 `json:"stake_age_days"`
	StakeUSD               float64 `json:"stake_usd"`
	AccountAgeDays         float64 `json:"account_age_days"`
	SettledTxCount         int     `json:"settled_tx_count"`
	HistoricalSlashPenalty float64 `json:"historical_slash_penalty"`
	DaysSinceLastSlash     float64 `json:"days_since_last_slash"`
}

// ClampScore constrains a score to the legal reputation range.
//
// Pre-conditions: score is any float64, including NaN and infinities.
// Post-conditions: returns MinTrustScore for NaN or negative input,
// MaxTrustScore above the ceiling, and score otherwise.
func ClampScore(score float64) float64 {
	if math.IsNaN(score) || score < MinTrustScore {
		return MinTrustScore
	}
	if score > MaxTrustScore {
		return MaxTrustScore
	}
	return score
}

// ComputeCompositeScore aggregates the 4 weighted components minus the decayed
// slash penalty into a single trust score.
//
// Pre-conditions: m is any Metrics value; empty history falls back to the
// DefaultInitialScore baseline of 2.5 for the components lacking evidence.
// Post-conditions: the result is in [MinTrustScore, MaxTrustScore], is
// monotonic non-decreasing in each component and non-increasing in the penalty.
func ComputeCompositeScore(m Metrics) float64 {
	history := m.FulfilledCount + m.NoShowCount
	fulfillment, cancel, peer := DefaultInitialScore, DefaultInitialScore, DefaultInitialScore
	if history > 0 {
		fulfillment = MaxTrustScore * float64(m.FulfilledCount) / float64(history)
	}
	if history > 0 || m.AvgCancelLeadTimeHours != 0 {
		cancel = normalized(m.AvgCancelLeadTimeHours, cancelFullHours) * MaxTrustScore
	}
	if m.FulfilledCount > 0 || m.PeerReviewAvg != 0 {
		peer = ClampScore(m.PeerReviewAvg)
	}
	seniority := normalized(m.StakeAgeDays, seniorityFullDays) * MaxTrustScore
	raw := weightFulfillment*fulfillment + weightCancel*cancel + weightPeer*peer + weightSeniority*seniority
	penalty := DecayedSlashPenalty(m.HistoricalSlashPenalty, m.DaysSinceLastSlash, DefaultDecayHalfLifeDays)
	return ClampScore(raw - penalty)
}

// ApplyWeightOfStakeEndorsements adds the stake-weighted endorsement delta:
// T_new = clamp(T_old + sum(R_i*S_i*V_i) / (N*ScaleFactor), 0.0, 5.0).
//
// Pre-conditions: endorsements may be empty or nil; scaleFactor <= 0 selects
// DefaultScaleFactor; negative R_i, S_i or V_i are treated as 0.
// Post-conditions: the result is in [MinTrustScore, MaxTrustScore]; zero-stake
// or zero-volume Sybil endorsements leave the score bitwise unchanged.
func ApplyWeightOfStakeEndorsements(currentScore float64, endorsements []Endorsement, scaleFactor float64) float64 {
	if len(endorsements) == 0 {
		return ClampScore(currentScore)
	}
	if scaleFactor <= 0 || math.IsNaN(scaleFactor) {
		scaleFactor = DefaultScaleFactor
	}
	var sum float64
	for _, e := range endorsements {
		r, s, v := e.EndorserScore, e.EndorserStakeUSD, e.TransactionVolumeUSD
		if r > 0 && s > 0 && v > 0 {
			sum += r * s * v
		}
	}
	return ClampScore(ClampScore(currentScore) + sum/(float64(len(endorsements))*scaleFactor))
}

// ComputeVelocityCapacity prices how much exposure an agent may book.
//
// Pre-conditions: stakeUSD, accountAgeDays and settledTxCount may be negative
// (clamped to 0); qualityWeight <= 0 selects DefaultQualityWeight.
// Post-conditions: returns (StakeUSD*AccountAgeDays)+(SettledTxCount*
// QualityWeight) >= 0, so a brand new agent has zero capacity regardless of
// stake, which is the whale defense.
func ComputeVelocityCapacity(stakeUSD, accountAgeDays float64, settledTxCount int, qualityWeight float64) float64 {
	if stakeUSD < 0 {
		stakeUSD = 0
	}
	if accountAgeDays < 0 {
		accountAgeDays = 0
	}
	if settledTxCount < 0 {
		settledTxCount = 0
	}
	if qualityWeight <= 0 || math.IsNaN(qualityWeight) {
		qualityWeight = DefaultQualityWeight
	}
	return stakeUSD*accountAgeDays + float64(settledTxCount)*qualityWeight
}

// DecayedSlashPenalty applies the exponential "Right to be Forgotten" decay
// Penalty(t) = initialPenalty * 2^(-t/halfLifeDays).
//
// Pre-conditions: initialPenalty <= 0 yields no penalty; halfLifeDays <= 0
// selects DefaultDecayHalfLifeDays; daysElapsed <= 0 returns the raw penalty.
// Post-conditions: returns a value in [0, initialPenalty] that is non-increasing
// in daysElapsed and halves every halfLifeDays.
func DecayedSlashPenalty(initialPenalty, daysElapsed, halfLifeDays float64) float64 {
	if initialPenalty <= 0 || math.IsNaN(initialPenalty) {
		return 0
	}
	if daysElapsed <= 0 {
		return initialPenalty
	}
	if halfLifeDays <= 0 || math.IsNaN(halfLifeDays) {
		halfLifeDays = DefaultDecayHalfLifeDays
	}
	return initialPenalty * math.Pow(2.0, -daysElapsed/halfLifeDays)
}

// IsProbationary reports whether a score is below the probation gate.
//
// Pre-conditions: score is any float64 (NaN is treated as probationary).
// Post-conditions: returns score < ProbationaryThreshold, the flag surfaced by
// Snapshot.Probationary to gating layers.
func IsProbationary(score float64) bool {
	return ClampScore(score) < ProbationaryThreshold
}

// normalized scales value over horizon into [0.0, 1.0].
//
// Pre-conditions: horizon should be > 0; value may be negative.
// Post-conditions: returns Min(1, Max(0, value/horizon)), or 0 for a
// non-positive horizon.
func normalized(value, horizon float64) float64 {
	if horizon <= 0 || math.IsNaN(horizon) {
		return 0
	}
	return math.Min(1.0, math.Max(0.0, value/horizon))
}
