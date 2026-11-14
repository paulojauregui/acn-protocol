package reputation

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"testing"
)

// almost reports whether got and want differ by more than tol.
//
// Pre-conditions: tol >= 0.
// Post-conditions: returns true when |got-want| > tol, for float assertions.
func almost(got, want, tol float64) bool {
	return math.Abs(got-want) > tol
}

// TestComputeCompositeScoreFourComponents pins the 4-component weighted math.
//
// Pre-conditions: math.go constants are the spec values.
// Post-conditions: weights apply as 0.50/0.20/0.20/0.10, empty history falls
// back to the 2.5 baselines and the slash penalty is subtracted before clamping.
func TestComputeCompositeScoreFourComponents(t *testing.T) {
	m := Metrics{AgentDID: "did:acn:a", FulfilledCount: 8, NoShowCount: 2,
		AvgCancelLeadTimeHours: 12, PeerReviewAvg: 4.0, StakeAgeDays: 90}
	if got, want := ComputeCompositeScore(m), 3.55; almost(got, want, 1e-9) {
		t.Fatalf("composite = %v, want %v", got, want)
	}
	if got, want := ComputeCompositeScore(Metrics{AgentDID: "x"}), 2.25; almost(got, want, 1e-9) {
		t.Fatalf("empty history composite = %v, want %v (seniority has no history)", got, want)
	}
	penalized := Metrics{AgentDID: "x", FulfilledCount: 4, NoShowCount: 1, AvgCancelLeadTimeHours: 24,
		PeerReviewAvg: 5, StakeAgeDays: 180, HistoricalSlashPenalty: 3.0}
	if got, want := ComputeCompositeScore(penalized), 1.5; almost(got, want, 1e-9) {
		t.Fatalf("penalized composite = %v, want %v", got, want)
	}
	ceiling := Metrics{AgentDID: "x", FulfilledCount: 5, AvgCancelLeadTimeHours: 48, PeerReviewAvg: 99, StakeAgeDays: 9e9}
	if got := ComputeCompositeScore(ceiling); got != MaxTrustScore {
		t.Fatalf("clamped composite = %v, want %v", got, MaxTrustScore)
	}
	if ClampScore(math.NaN()) != MinTrustScore || ClampScore(-1) != MinTrustScore || ClampScore(7) != MaxTrustScore {
		t.Fatalf("ClampScore out of range: %v %v %v", ClampScore(math.NaN()), ClampScore(-1), ClampScore(7))
	}
	if !IsProbationary(2.49) || IsProbationary(2.5) || !IsProbationary(math.NaN()) {
		t.Fatal("IsProbationary gate mismatch")
	}
}

// TestSybilWashTradingDefenseProvesZeroStakeEndorsementsAreFree zero-stake or
// zero-volume endorsements must never mint reputation.
//
// Pre-conditions: 1,000 endorsements from high-score endorsers with S_i = 0 or
// V_i = 0, plus a control set with real stake and volume.
// Post-conditions: the Sybil batches add exactly 0.0 while the staked batch
// raises the score, proving Weight-of-Stake resists wash-trading Sybils.
func TestSybilWashTradingDefenseProvesZeroStakeEndorsementsAreFree(t *testing.T) {
	base := 3.0
	sybilStake, sybilVolume, honest := make([]Endorsement, 1000), make([]Endorsement, 1000), make([]Endorsement, 1000)
	for i := range sybilStake {
		sybilStake[i] = Endorsement{EndorserDID: "did:acn:sybil", EndorserScore: 5, EndorserStakeUSD: 0, TransactionVolumeUSD: 1e6}
		sybilVolume[i] = Endorsement{EndorserDID: "did:acn:sybil", EndorserScore: 5, EndorserStakeUSD: 1e6, TransactionVolumeUSD: 0}
		honest[i] = Endorsement{EndorserDID: "did:acn:peer", EndorserScore: 5, EndorserStakeUSD: 2000, TransactionVolumeUSD: 5000}
	}
	if got := ApplyWeightOfStakeEndorsements(base, sybilStake, DefaultScaleFactor); got != base {
		t.Fatalf("zero-stake Sybil boost = %v, want no change (%v)", got, base)
	}
	if got := ApplyWeightOfStakeEndorsements(base, sybilVolume, DefaultScaleFactor); got != base {
		t.Fatalf("zero-volume Sybil boost = %v, want no change (%v)", got, base)
	}
	if got := ApplyWeightOfStakeEndorsements(base, honest, DefaultScaleFactor); got <= base {
		t.Fatalf("staked endorsements = %v, want > %v", got, base)
	}
	if got := ApplyWeightOfStakeEndorsements(base, nil, 0); got != base {
		t.Fatalf("empty endorsements = %v, want %v", got, base)
	}

	e := NewEngine()
	if _, err := e.UpsertMetrics(Metrics{AgentDID: "sybil", FulfilledCount: 3, NoShowCount: 1, PeerReviewAvg: 5, AvgCancelLeadTimeHours: 24}); err != nil {
		t.Fatalf("UpsertMetrics: %v", err)
	}
	before, err := e.Evaluate("sybil")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	snap, err := e.ApplyEndorsements("sybil", sybilStake)
	if err != nil {
		t.Fatalf("ApplyEndorsements: %v", err)
	}
	if snap.CompositeScore != before.CompositeScore {
		t.Fatalf("engine Sybil score = %v, want %v", snap.CompositeScore, before.CompositeScore)
	}
	staked, err := e.ApplyEndorsements("sybil", honest)
	if err != nil {
		t.Fatalf("ApplyEndorsements staked: %v", err)
	}
	if staked.CompositeScore <= before.CompositeScore {
		t.Fatalf("engine staked score = %v, want > %v", staked.CompositeScore, before.CompositeScore)
	}
}

// TestWhaleVelocityDefenseBlocksZeroAgeAgent proves stake alone buys no speed.
//
// Pre-conditions: a whale with 1,000,000 USD stake, AccountAgeDays = 0 and
// SettledTxCount = 0 requests a 5,000 USD exposure.
// Post-conditions: capacity is 0 and the booking is denied until account age
// and settled history accrue, at which point it is allowed.
func TestWhaleVelocityDefenseBlocksZeroAgeAgent(t *testing.T) {
	if got := ComputeVelocityCapacity(1_000_000, 0, 0, DefaultQualityWeight); got != 0 {
		t.Fatalf("capacity at t=0 = %v, want 0", got)
	}
	if got := ComputeVelocityCapacity(-5, -5, -5, -1); got != 0 {
		t.Fatalf("negative capacity = %v, want 0", got)
	}
	e := NewEngine()
	if _, err := e.UpsertMetrics(Metrics{AgentDID: "whale", StakeUSD: 1_000_000}); err != nil {
		t.Fatalf("UpsertMetrics: %v", err)
	}
	allowed, capacity, err := e.CheckVelocityAllowed("whale", 5_000)
	if err != nil {
		t.Fatalf("CheckVelocityAllowed: %v", err)
	}
	if allowed || capacity != 0 {
		t.Fatalf("whale at t=0 allowed=%v capacity=%v, want false/0", allowed, capacity)
	}
	if _, err := e.AdvanceTimeDays("whale", 10); err != nil {
		t.Fatalf("AdvanceTimeDays: %v", err)
	}
	if _, err := e.RecordSettlement("whale", true, 4.5, 0); err != nil {
		t.Fatalf("RecordSettlement: %v", err)
	}
	allowed, capacity, err = e.CheckVelocityAllowed("whale", 5_000)
	if err != nil {
		t.Fatalf("CheckVelocityAllowed after aging: %v", err)
	}
	if !allowed || almost(capacity, 10_000_025, 1e-6) {
		t.Fatalf("aged whale allowed=%v capacity=%v, want true/10000025", allowed, capacity)
	}
}

// TestSlashDecayRecoveryOutlivesProbation proves the Right to be Forgotten.
//
// Pre-conditions: an agent carries a 3.0 historical slash penalty that pushes
// it below ProbationaryThreshold.
// Post-conditions: AdvanceTimeDays decays the penalty exponentially (half every
// DefaultDecayHalfLifeDays) until the agent leaves probation.
func TestSlashDecayRecoveryOutlivesProbation(t *testing.T) {
	if got := DecayedSlashPenalty(0, 10, 30); got != 0 {
		t.Fatalf("zero penalty = %v, want 0", got)
	}
	if got := DecayedSlashPenalty(4, -1, 30); got != 4 {
		t.Fatalf("negative elapsed = %v, want 4", got)
	}
	if got := DecayedSlashPenalty(4, 30, 0); almost(got, 2.0, 1e-9) {
		t.Fatalf("one half-life = %v, want 2.0 (default half-life)", got)
	}
	e := NewEngine()
	if _, err := e.UpsertMetrics(Metrics{AgentDID: "ash", StakeUSD: 500, AccountAgeDays: 10,
		SettledTxCount: 4, FulfilledCount: 4, AvgCancelLeadTimeHours: 24, PeerReviewAvg: 5,
		StakeAgeDays: 180, HistoricalSlashPenalty: 3.0}); err != nil {
		t.Fatalf("UpsertMetrics: %v", err)
	}
	snap, err := e.Evaluate("ash")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !snap.Probationary || almost(snap.CompositeScore, 2.0, 1e-9) {
		t.Fatalf("slash score = %v probationary=%v, want 2.0/true", snap.CompositeScore, snap.Probationary)
	}
	if _, err := e.AdvanceTimeDays("ash", 30); err != nil {
		t.Fatalf("AdvanceTimeDays: %v", err)
	}
	recovered, err := e.Evaluate("ash")
	if err != nil {
		t.Fatalf("Evaluate after decay: %v", err)
	}
	if recovered.Probationary || almost(recovered.CompositeScore, 3.5, 1e-9) {
		t.Fatalf("recovered score = %v probationary=%v, want 3.5/false", recovered.CompositeScore, recovered.Probationary)
	}
	if recovered.Metrics.StakeAgeDays != 210 || recovered.Metrics.DaysSinceLastSlash != 30 || recovered.Metrics.AccountAgeDays != 40 {
		t.Fatalf("aged metrics = %+v", recovered.Metrics)
	}
	if _, err := e.RecordSettlement("ash", false, 0, 0); err != nil {
		t.Fatalf("RecordSettlement no-show: %v", err)
	}
	slashed, err := e.Evaluate("ash")
	if err != nil {
		t.Fatalf("Evaluate after slash: %v", err)
	}
	if slashed.Metrics.NoShowCount != 1 || slashed.Metrics.DaysSinceLastSlash != 0 ||
		almost(slashed.Metrics.HistoricalSlashPenalty, 4.25, 1e-9) {
		t.Fatalf("slash metrics = %+v", slashed.Metrics)
	}
}

// TestEngineContractAndConcurrency covers error paths and -race safety.
//
// Pre-conditions: a fresh Engine plus one unregistered agent identifier.
// Post-conditions: blank identifiers return ErrEmptyAgentDID, unknown agents
// wrap ErrUnknownAgent, and 8 concurrent writers produce no data race.
func TestEngineContractAndConcurrency(t *testing.T) {
	e := NewEngine()
	if _, err := e.UpsertMetrics(Metrics{}); !errors.Is(err, ErrEmptyAgentDID) {
		t.Fatalf("UpsertMetrics err = %v, want ErrEmptyAgentDID", err)
	}
	if _, err := e.RecordSettlement("", true, 5, 5); !errors.Is(err, ErrEmptyAgentDID) {
		t.Fatalf("RecordSettlement err = %v, want ErrEmptyAgentDID", err)
	}
	if _, err := e.ApplyEndorsements("", nil); !errors.Is(err, ErrEmptyAgentDID) {
		t.Fatalf("ApplyEndorsements err = %v, want ErrEmptyAgentDID", err)
	}
	if _, err := e.AdvanceTimeDays("ghost", 1); !errors.Is(err, ErrUnknownAgent) {
		t.Fatalf("AdvanceTimeDays err = %v, want ErrUnknownAgent", err)
	}
	if _, _, err := e.CheckVelocityAllowed("ghost", 1); !errors.Is(err, ErrUnknownAgent) {
		t.Fatalf("CheckVelocityAllowed err = %v, want ErrUnknownAgent", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			did := fmt.Sprintf("did:acn:agent-%d", i%2)
			if _, err := e.UpsertMetrics(Metrics{AgentDID: did, StakeUSD: 100, AccountAgeDays: 3}); err != nil {
				t.Errorf("UpsertMetrics: %v", err)
			}
			if _, err := e.RecordSettlement(did, i%3 != 0, float64(i), float64(i)); err != nil {
				t.Errorf("RecordSettlement: %v", err)
			}
			if _, err := e.ApplyEndorsements(did, []Endorsement{{EndorserDID: "p", EndorserScore: 4, EndorserStakeUSD: 900, TransactionVolumeUSD: 400}}); err != nil {
				t.Errorf("ApplyEndorsements: %v", err)
			}
			if _, err := e.AdvanceTimeDays(did, 2); err != nil {
				t.Errorf("AdvanceTimeDays: %v", err)
			}
			if _, _, err := e.CheckVelocityAllowed(did, 10); err != nil {
				t.Errorf("CheckVelocityAllowed: %v", err)
			}
			if snap, err := e.Evaluate(did); err != nil || snap.AgentDID != did ||
				snap.CompositeScore < MinTrustScore || snap.CompositeScore > MaxTrustScore {
				t.Errorf("Evaluate = %+v, %v", snap, err)
			}
		}(i)
	}
	wg.Wait()
}
