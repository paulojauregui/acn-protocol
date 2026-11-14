package simulator_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/paulojauregui/acn-protocol/internal/simulator"
)

// TestRunSybilSwarmSimulation verifies that 1,000 zero-stake Sybil bots wash-trading zero-value
// endorsements remain trapped at Tier 0 Ephemeral with 0 high-value bookings approved,
// while 10 honest bonded agents achieve scores >= 4.2 and qualify for Tier 2/3.
//
// Pre-conditions: simulator package is compiled and initialized.
// Post-conditions: DefenseSucceeded is true, all 1,000 Sybil bots are constrained,
// honest agents achieve high reputation, and execution duration is bounded under 50ms.
func TestRunSybilSwarmSimulation(t *testing.T) {
	res := simulator.RunSybilSwarmSimulation()
	if res == nil {
		t.Fatal("expected non-nil SimulationResult")
	}

	if !res.DefenseSucceeded {
		t.Fatalf("expected DefenseSucceeded to be true, got false. Summary: %s", res.Summary)
	}

	if res.DurationMs > 50.0 {
		t.Errorf("simulation duration %.2fms exceeded 50ms requirement", res.DurationMs)
	}

	if res.ExecutedAt.IsZero() || time.Since(res.ExecutedAt) > 10*time.Second {
		t.Errorf("invalid ExecutedAt timestamp: %v", res.ExecutedAt)
	}

	sybilCount, ok := res.Metrics["sybil_bot_count"].(int)
	if !ok || sybilCount != 1000 {
		t.Errorf("expected sybil_bot_count == 1000, got %v", res.Metrics["sybil_bot_count"])
	}

	honestCount, ok := res.Metrics["honest_agent_count"].(int)
	if !ok || honestCount != 10 {
		t.Errorf("expected honest_agent_count == 10, got %v", res.Metrics["honest_agent_count"])
	}

	sybilAvgScore, ok := res.Metrics["sybil_avg_score"].(float64)
	if !ok || sybilAvgScore > 2.5 {
		t.Errorf("expected sybil_avg_score <= 2.5, got %v", res.Metrics["sybil_avg_score"])
	}

	honestAvgScore, ok := res.Metrics["honest_avg_score"].(float64)
	if !ok || honestAvgScore < 4.2 {
		t.Errorf("expected honest_avg_score >= 4.2, got %v", res.Metrics["honest_avg_score"])
	}

	trappedPct, ok := res.Metrics["sybil_tier_0_trapped_pct"].(float64)
	if !ok || trappedPct != 100.0 {
		t.Errorf("expected sybil_tier_0_trapped_pct == 100.0, got %v", res.Metrics["sybil_tier_0_trapped_pct"])
	}

	highValueApproved, ok := res.Metrics["sybil_high_value_bookings_approved"].(int)
	if !ok || highValueApproved != 0 {
		t.Errorf("expected sybil_high_value_bookings_approved == 0, got %v", res.Metrics["sybil_high_value_bookings_approved"])
	}

	// Verify JSON serialization consistency.
	data, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("failed to marshal SimulationResult: %v", err)
	}
	var roundTrip simulator.SimulationResult
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatalf("failed to unmarshal SimulationResult: %v", err)
	}
	if roundTrip.Scenario != res.Scenario || roundTrip.DefenseSucceeded != res.DefenseSucceeded {
		t.Errorf("JSON round-trip mismatch: got %+v, want %+v", roundTrip, res)
	}
}

// TestRunWhaleAttackSimulation verifies that a brand-new Whale depositing $1,000,000 has zero
// velocity capacity at t=0, throttling 498+ of 500 concurrent high-value bookings until age accrues.
//
// Pre-conditions: simulator package is compiled and initialized.
// Post-conditions: DefenseSucceeded is true, at least 498 bookings are throttled at t=0,
// velocity capacity scales up by Day 30, and execution duration is bounded under 50ms.
func TestRunWhaleAttackSimulation(t *testing.T) {
	res := simulator.RunWhaleAttackSimulation()
	if res == nil {
		t.Fatal("expected non-nil SimulationResult")
	}

	if !res.DefenseSucceeded {
		t.Fatalf("expected DefenseSucceeded to be true, got false. Summary: %s", res.Summary)
	}

	if res.DurationMs > 50.0 {
		t.Errorf("simulation duration %.2fms exceeded 50ms requirement", res.DurationMs)
	}

	whaleDeposit, ok := res.Metrics["whale_deposit_usd"].(float64)
	if !ok || whaleDeposit != 1000000.0 {
		t.Errorf("expected whale_deposit_usd == 1000000.0, got %v", res.Metrics["whale_deposit_usd"])
	}

	attempted, ok := res.Metrics["attempted_concurrent_bookings"].(int)
	if !ok || attempted != 500 {
		t.Errorf("expected attempted_concurrent_bookings == 500, got %v", res.Metrics["attempted_concurrent_bookings"])
	}

	approvedT0, ok := res.Metrics["approved_bookings_at_t0"].(int)
	if !ok || approvedT0 > 2 {
		t.Errorf("expected approved_bookings_at_t0 <= 2, got %v", res.Metrics["approved_bookings_at_t0"])
	}

	blockedT0, ok := res.Metrics["blocked_bookings_at_t0"].(int)
	if !ok || blockedT0 < 498 {
		t.Errorf("expected blocked_bookings_at_t0 >= 498, got %v", res.Metrics["blocked_bookings_at_t0"])
	}

	capT0, ok := res.Metrics["velocity_capacity_t0_usd"].(float64)
	if !ok || capT0 != 0.0 {
		t.Errorf("expected velocity_capacity_t0_usd == 0.0, got %v", res.Metrics["velocity_capacity_t0_usd"])
	}

	capDay30, ok := res.Metrics["velocity_capacity_day30_usd"].(float64)
	if !ok || capDay30 < 50000.0 {
		t.Errorf("expected velocity_capacity_day30_usd >= 50000.0, got %v", res.Metrics["velocity_capacity_day30_usd"])
	}
}

// TestRunCollusiveVendorSimulation verifies that a malicious vendor submitting a false No-Show claim
// against an honest user with verified beacon and POS proof is detected by the Oracle, arbitrated
// by the Jury in favor of the user, has the user bond returned intact, and penalizes vendor reputation.
//
// Pre-conditions: simulator package is compiled and initialized.
// Post-conditions: DefenseSucceeded is true, Oracle status is STATUS_DISPUTED, Jury winner is USER,
// user bond is not slashed, vendor is slashed with lowered reputation, and duration is bounded under 50ms.
func TestRunCollusiveVendorSimulation(t *testing.T) {
	res := simulator.RunCollusiveVendorSimulation()
	if res == nil {
		t.Fatal("expected non-nil SimulationResult")
	}

	if !res.DefenseSucceeded {
		t.Fatalf("expected DefenseSucceeded to be true, got false. Summary: %s", res.Summary)
	}

	if res.DurationMs > 50.0 {
		t.Errorf("simulation duration %.2fms exceeded 50ms requirement", res.DurationMs)
	}

	oracleStatus, ok := res.Metrics["oracle_status"].(string)
	if !ok || oracleStatus != "STATUS_DISPUTED" {
		t.Errorf("expected oracle_status == STATUS_DISPUTED, got %v", res.Metrics["oracle_status"])
	}

	juryWinner, ok := res.Metrics["jury_winner"].(string)
	if !ok || juryWinner != "USER" {
		t.Errorf("expected jury_winner == USER, got %v", res.Metrics["jury_winner"])
	}

	userBondSlashed, ok := res.Metrics["user_bond_slashed"].(bool)
	if !ok || userBondSlashed {
		t.Errorf("expected user_bond_slashed == false, got %v", res.Metrics["user_bond_slashed"])
	}

	vendorSlashed, ok := res.Metrics["vendor_slashed"].(bool)
	if !ok || !vendorSlashed {
		t.Errorf("expected vendor_slashed == true, got %v", res.Metrics["vendor_slashed"])
	}

	repBefore, okBefore := res.Metrics["vendor_reputation_before"].(float64)
	repAfter, okAfter := res.Metrics["vendor_reputation_after"].(float64)
	if !okBefore || !okAfter || repAfter >= repBefore {
		t.Errorf("expected vendor_reputation_after < vendor_reputation_before, got before=%v, after=%v", repBefore, repAfter)
	}
}
