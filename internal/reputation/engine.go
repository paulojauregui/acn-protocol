package reputation

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// Sentinel errors returned by the Engine.
//
// Pre-conditions: none (package-level values, never mutated).
// Post-conditions: every Engine failure wraps one of these so callers can use
// errors.Is instead of string matching.
var (
	// ErrEmptyAgentDID is returned when an agent identifier is blank.
	ErrEmptyAgentDID = errors.New("reputation: agent did must not be empty")
	// ErrUnknownAgent is returned when an identifier has no stored metrics.
	ErrUnknownAgent = errors.New("reputation: unknown agent")
)

// Snapshot is an immutable evaluation of one agent at a point in time.
//
// Pre-conditions: produced only by Engine methods, never hand-built by callers.
// Post-conditions: CompositeScore is in [MinTrustScore, MaxTrustScore],
// FulfillmentRate is in [0.0, 1.0], VelocityCapacityUSD >= 0, Probationary
// matches IsProbationary(CompositeScore) and Metrics is a deep copy, so
// serializing it never races the Engine.
type Snapshot struct {
	AgentDID            string    `json:"agent_did"`
	CompositeScore      float64   `json:"composite_score"`
	FulfillmentRate     float64   `json:"fulfillment_rate"`
	VelocityCapacityUSD float64   `json:"velocity_capacity_usd"`
	Probationary        bool      `json:"probationary"`
	Metrics             Metrics   `json:"metrics"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// Engine is the concurrency-safe, in-memory reputation store.
//
// It is the single writer of reputation state: every mutation (settlements,
// endorsements, time advances) runs under the write lock, so the stored boost
// and the derived snapshot can never drift apart.
//
// Pre-conditions: constructed via NewEngine for a usable map set; a zero Engine
// is safe to read but rejects writes into a nil map.
// Post-conditions: all exported methods take the lock exactly once, never call
// user code and never expose internal pointers, so the type is race-free under
// go test -race for any number of goroutines.
type Engine struct {
	mu               sync.RWMutex
	records          map[string]Metrics
	endorsementBoost map[string]float64
}

// NewEngine allocates an empty Engine ready for use.
//
// Pre-conditions: none.
// Post-conditions: the returned Engine holds zero agents, is immediately usable
// concurrently and shares no state with any other Engine.
func NewEngine() *Engine {
	return &Engine{
		records:          make(map[string]Metrics),
		endorsementBoost: make(map[string]float64),
	}
}

// UpsertMetrics stores or replaces an agent's metrics and returns its snapshot.
//
// Pre-conditions: m.AgentDID is non-empty after trimming; counters are >= 0.
// Post-conditions: on success the record is stored verbatim (clamped counters
// only) and a fresh Snapshot is returned; on failure ErrEmptyAgentDID is
// returned and no state is mutated.
func (e *Engine) UpsertMetrics(m Metrics) (*Snapshot, error) {
	if m.AgentDID == "" {
		return nil, ErrEmptyAgentDID
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	m.FulfilledCount = nonNegativeInt(m.FulfilledCount)
	m.NoShowCount = nonNegativeInt(m.NoShowCount)
	m.SettledTxCount = nonNegativeInt(m.SettledTxCount)
	e.records[m.AgentDID] = m
	return e.snapshotLocked(m.AgentDID), nil
}

// RecordSettlement applies one settlement outcome to an agent's history.
//
// Pre-conditions: agentDID is non-empty; peerRating is any value (clamped to
// [0.0, 5.0]); cancelLeadHours is the hours of notice given (negative clamped
// to 0); an unknown agentDID creates its record.
// Post-conditions: when fulfilled, FulfilledCount and SettledTxCount grow by 1
// and PeerReviewAvg and AvgCancelLeadTimeHours become running averages over the
// fulfilled settlements; otherwise NoShowCount grows by 1,
// HistoricalSlashPenalty grows by SlashPenaltyPerNoShow and
// DaysSinceLastSlash resets to 0. A fresh Snapshot is returned.
func (e *Engine) RecordSettlement(agentDID string, fulfilled bool, peerRating float64, cancelLeadHours float64) (*Snapshot, error) {
	if agentDID == "" {
		return nil, ErrEmptyAgentDID
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	m := e.records[agentDID]
	m.AgentDID = agentDID
	if fulfilled {
		m.FulfilledCount++
		m.SettledTxCount++
		n := float64(m.FulfilledCount)
		m.PeerReviewAvg += (ClampScore(peerRating) - m.PeerReviewAvg) / n
		if cancelLeadHours > 0 {
			m.AvgCancelLeadTimeHours += (cancelLeadHours - m.AvgCancelLeadTimeHours) / n
		}
	} else {
		m.NoShowCount++
		m.HistoricalSlashPenalty += SlashPenaltyPerNoShow
		m.DaysSinceLastSlash = 0
	}
	e.records[agentDID] = m
	return e.snapshotLocked(agentDID), nil
}

// ApplyEndorsements stores the Weight-of-Stake boost earned from endorsements.
//
// Pre-conditions: agentDID is non-empty; endorsements may be empty (boost
// resets to 0); unknown agentDID is created before evaluation.
// Post-conditions: endorsementBoost[agentDID] equals
// ApplyWeightOfStakeEndorsements(base, endorsements, DefaultScaleFactor) minus
// base, so the boost is replaced rather than accumulated, and the final score
// stays inside [MinTrustScore, MaxTrustScore]. Sybil endorsements (S_i = 0 or
// V_i = 0) leave the boost at 0.
func (e *Engine) ApplyEndorsements(agentDID string, endorsements []Endorsement) (*Snapshot, error) {
	if agentDID == "" {
		return nil, ErrEmptyAgentDID
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	m, ok := e.records[agentDID]
	if !ok {
		m = Metrics{AgentDID: agentDID}
		e.records[agentDID] = m
	}
	base := ComputeCompositeScore(m)
	boosted := ApplyWeightOfStakeEndorsements(base, endorsements, DefaultScaleFactor)
	e.endorsementBoost[agentDID] = boosted - base
	return e.snapshotLocked(agentDID), nil
}

// AdvanceTimeDays ages an agent, demonstrating slash decay recovery.
//
// Pre-conditions: agentDID is non-empty and known; days <= 0 is a no-op read.
// Post-conditions: AccountAgeDays and DaysSinceLastSlash grow by days,
// StakeAgeDays grows by days only when StakeUSD > 0, and the returned Snapshot
// reflects the smaller decayed penalty (higher score over time).
func (e *Engine) AdvanceTimeDays(agentDID string, days float64) (*Snapshot, error) {
	if agentDID == "" {
		return nil, ErrEmptyAgentDID
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	m, ok := e.records[agentDID]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownAgent, agentDID)
	}
	if days > 0 {
		m.AccountAgeDays += days
		m.DaysSinceLastSlash += days
		if m.StakeUSD > 0 {
			m.StakeAgeDays += days
		}
		e.records[agentDID] = m
	}
	return e.snapshotLocked(agentDID), nil
}

// Evaluate recomputes and returns the current snapshot of an agent.
//
// Pre-conditions: agentDID is non-empty and known to the Engine.
// Post-conditions: returns a deep-copy Snapshot without mutating state, or
// ErrEmptyAgentDID / an error wrapping ErrUnknownAgent.
func (e *Engine) Evaluate(agentDID string) (*Snapshot, error) {
	if agentDID == "" {
		return nil, ErrEmptyAgentDID
	}
	e.mu.RLock()
	defer e.mu.RUnlock()

	if _, ok := e.records[agentDID]; !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownAgent, agentDID)
	}
	return e.snapshotLocked(agentDID), nil
}

// CheckVelocityAllowed gates a requested exposure against Time-Weighted
// Velocity capacity.
//
// Pre-conditions: agentDID is non-empty and known; requestedExposureUSD <= 0 is
// always allowed.
// Post-conditions: returns (requestedExposureUSD <= snap.VelocityCapacityUSD,
// snap.VelocityCapacityUSD, nil); capacity is 0 for t = 0 agents, which blocks
// whale bookings regardless of stake size.
func (e *Engine) CheckVelocityAllowed(agentDID string, requestedExposureUSD float64) (bool, float64, error) {
	snap, err := e.Evaluate(agentDID)
	if err != nil {
		return false, 0, err
	}
	return requestedExposureUSD <= snap.VelocityCapacityUSD, snap.VelocityCapacityUSD, nil
}

// snapshotLocked builds the Snapshot for a stored agent.
//
// Pre-conditions: e.mu is held (read or write) and records[agentDID] exists.
// Post-conditions: returns a value with no aliasing to internal maps, score
// ClampScore(base+boost), rate 0.5 when the agent has no history and
// UpdatedAt == time.Now().UTC().
func (e *Engine) snapshotLocked(agentDID string) *Snapshot {
	m := e.records[agentDID]
	base := ComputeCompositeScore(m)
	score := ClampScore(base + e.endorsementBoost[agentDID])
	rate := 0.5
	if history := m.FulfilledCount + m.NoShowCount; history > 0 {
		rate = float64(m.FulfilledCount) / float64(history)
	}
	return &Snapshot{
		AgentDID:            m.AgentDID,
		CompositeScore:      score,
		FulfillmentRate:     rate,
		VelocityCapacityUSD: ComputeVelocityCapacity(m.StakeUSD, m.AccountAgeDays, m.SettledTxCount, DefaultQualityWeight),
		Probationary:        IsProbationary(score),
		Metrics:             m,
		UpdatedAt:           time.Now().UTC(),
	}
}

// nonNegativeInt clamps a counter to zero or above.
//
// Pre-conditions: v is any int.
// Post-conditions: returns v when v >= 0, otherwise 0.
func nonNegativeInt(v int) int {
	if v < 0 {
		return 0
	}
	return v
}
