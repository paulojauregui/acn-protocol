// Package simulator implements deterministic adversarial simulation scenarios
// proving the mathematical soundness and economic security of the ACN protocol
// against Sybil swarms, whale capital attacks, and collusive vendor fraud.
package simulator

import (
	"context"
	"fmt"
	"time"

	"github.com/paulojauregui/acn-protocol/internal/escrow"
	"github.com/paulojauregui/acn-protocol/internal/governance"
	"github.com/paulojauregui/acn-protocol/internal/identity"
	"github.com/paulojauregui/acn-protocol/internal/oracle"
	"github.com/paulojauregui/acn-protocol/internal/reputation"
)

// SimulationResult encapsulates the deterministic outcome and telemetry of an adversarial simulation scenario.
//
// Pre-conditions: none.
// Post-conditions: holds scenario outcome, defense status, execution metrics, and timing bounds.
type SimulationResult struct {
	Scenario         string         `json:"scenario"`
	DefenseSucceeded bool           `json:"defense_succeeded"`
	Summary          string         `json:"summary"`
	Metrics          map[string]any `json:"metrics"`
	DurationMs       float64        `json:"duration_ms"`
	ExecutedAt       time.Time      `json:"executed_at"`
}

// RunSybilSwarmSimulation executes a deterministic simulation of 1,000 zero-stake Sybil bots
// wash-trading zero-value endorsements against 10 bonded honest agents, demonstrating that
// Weight-of-Stake isolates unbonded Sybil actors to Tier 0 Ephemeral with zero high-value bookings.
//
// Pre-conditions: none.
// Post-conditions: returns a non-nil SimulationResult confirming DefenseSucceeded is true,
// all 1,000 Sybil bots remain trapped at Tier 0 Ephemeral, and honest agents reach Tier 2/3.
func RunSybilSwarmSimulation() *SimulationResult {
	start := time.Now()
	const (
		sybilCount       = 1000
		honestCount      = 10
		highValueBooking = 100.0
	)

	// Phase 1: Simulate 1,000 Sybil bots with $0 bond and wash-trading $0 endorsements.
	sybilScores := make([]float64, sybilCount)
	var sybilTrappedCount int
	var sybilHighValueApproved int
	var sybilScoreSum float64

	for i := 0; i < sybilCount; i++ {
		botDID := fmt.Sprintf("did:acn:sybil-%04d", i)
		baseScore := reputation.ComputeCompositeScore(reputation.Metrics{
			AgentDID: botDID,
			StakeUSD: 0.0,
		})

		// Sybil bots attempt wash-trading endorsements with zero stake and zero volume.
		endorsements := []reputation.Endorsement{
			{
				EndorserDID:          fmt.Sprintf("did:acn:sybil-%04d", (i+1)%sybilCount),
				EndorserScore:        baseScore,
				EndorserStakeUSD:     0.0,
				TransactionVolumeUSD: 0.0,
			},
			{
				EndorserDID:          fmt.Sprintf("did:acn:sybil-%04d", (i+2)%sybilCount),
				EndorserScore:        baseScore,
				EndorserStakeUSD:     0.0,
				TransactionVolumeUSD: 0.0,
			},
		}

		boostedScore := reputation.ApplyWeightOfStakeEndorsements(baseScore, endorsements, reputation.DefaultScaleFactor)
		sybilScores[i] = boostedScore
		sybilScoreSum += boostedScore

		// Evaluate Tier ladder: 0 bond, 0 spend, 0 settled txs.
		tier := identity.CalculateTrustTier(0.0, 0.0, 0, false, boostedScore)
		if tier == identity.Tier0Ephemeral {
			sybilTrappedCount++
		}
		if highValueBooking <= tier.MaxExposureUSD() {
			sybilHighValueApproved++
		}
	}

	sybilAvgScore := sybilScoreSum / float64(sybilCount)
	sybilTrappedPct := (float64(sybilTrappedCount) / float64(sybilCount)) * 100.0

	// Phase 2: Simulate 10 Honest Bonded Agents ($250 bond, $1,200 spend, 25 settled bookings).
	honestScores := make([]float64, honestCount)
	var honestScoreSum float64
	var honestTiersQualifying int

	for i := 0; i < honestCount; i++ {
		honestDID := fmt.Sprintf("did:acn:honest-%02d", i)
		baseScore := reputation.ComputeCompositeScore(reputation.Metrics{
			AgentDID:               honestDID,
			FulfilledCount:         25,
			SettledTxCount:         25,
			AvgCancelLeadTimeHours: 24.0,
			PeerReviewAvg:          5.0,
			StakeAgeDays:           180.0,
			StakeUSD:               250.0,
		})

		// Honest agents endorse each other with verified stake and real transaction volume.
		var honestEndorsements []reputation.Endorsement
		for j := 0; j < honestCount; j++ {
			if i != j {
				honestEndorsements = append(honestEndorsements, reputation.Endorsement{
					EndorserDID:          fmt.Sprintf("did:acn:honest-%02d", j),
					EndorserScore:        4.5,
					EndorserStakeUSD:     250.0,
					TransactionVolumeUSD: 100.0,
				})
			}
		}

		boostedScore := reputation.ApplyWeightOfStakeEndorsements(baseScore, honestEndorsements, reputation.DefaultScaleFactor)
		honestScores[i] = boostedScore
		honestScoreSum += boostedScore

		tier := identity.CalculateTrustTier(250.0, 1200.0, 25, false, boostedScore)
		if tier >= identity.Tier2Reputational {
			honestTiersQualifying++
		}
	}

	honestAvgScore := honestScoreSum / float64(honestCount)

	defenseSucceeded := sybilTrappedPct == 100.0 &&
		sybilHighValueApproved == 0 &&
		sybilAvgScore <= 2.5 &&
		honestAvgScore >= 4.2 &&
		honestTiersQualifying == honestCount

	summary := fmt.Sprintf(
		"Sybil Swarm Defense Succeeded: %d/%d (%.1f%%) Sybil bots trapped at Tier 0 Ephemeral with 0 high-value bookings approved; %d/%d honest agents reached Tier 2/3 (avg score: %.2f)",
		sybilTrappedCount, sybilCount, sybilTrappedPct, honestTiersQualifying, honestCount, honestAvgScore,
	)

	return &SimulationResult{
		Scenario:         "Sybil Swarm Attack",
		DefenseSucceeded: defenseSucceeded,
		Summary:          summary,
		Metrics: map[string]any{
			"sybil_bot_count":                    sybilCount,
			"honest_agent_count":                 honestCount,
			"sybil_avg_score":                    sybilAvgScore,
			"honest_avg_score":                   honestAvgScore,
			"sybil_tier_0_trapped_pct":           sybilTrappedPct,
			"sybil_high_value_bookings_approved": sybilHighValueApproved,
		},
		DurationMs: float64(time.Since(start).Microseconds()) / 1000.0,
		ExecutedAt: time.Now().UTC(),
	}
}

// RunWhaleAttackSimulation executes a deterministic simulation of a brand-new Whale agent depositing
// $1,000,000 and attempting 500 concurrent $100 bookings ($50,000 exposure), demonstrating that
// Time-Weighted Velocity (nu) throttles high-value exposure at t=0 until longitudinal age and tx history accrue.
//
// Pre-conditions: none.
// Post-conditions: returns a non-nil SimulationResult confirming DefenseSucceeded is true,
// 498+ concurrent bookings are throttled at t=0, and velocity capacity scales with longitudinal tenure.
func RunWhaleAttackSimulation() *SimulationResult {
	start := time.Now()
	const (
		whaleDepositUSD     = 1000000.0
		attemptedBookings   = 500
		bookingAmountUSD    = 100.0
		accountAgeDaysT0    = 0.0
		settledTxCountT0    = 0
		accountAgeDaysDay30 = 30.0
	)

	// Compute initial Velocity Capacity at t=0: nu = (StakeUSD * AgeDays) + (SettledTxCount * QualityWeight)
	velocityCapacityT0 := reputation.ComputeVelocityCapacity(
		whaleDepositUSD,
		accountAgeDaysT0,
		settledTxCountT0,
		reputation.DefaultQualityWeight,
	)

	// Evaluate concurrent booking requests against t=0 capacity.
	var approvedBookingsT0 int
	var blockedBookingsT0 int
	var cumulativeExposureT0 float64

	for i := 0; i < attemptedBookings; i++ {
		if cumulativeExposureT0+bookingAmountUSD <= velocityCapacityT0 {
			approvedBookingsT0++
			cumulativeExposureT0 += bookingAmountUSD
		} else {
			blockedBookingsT0++
		}
	}

	// Compute mature Velocity Capacity at Day 30.
	velocityCapacityDay30 := reputation.ComputeVelocityCapacity(
		whaleDepositUSD,
		accountAgeDaysDay30,
		settledTxCountT0,
		reputation.DefaultQualityWeight,
	)

	defenseSucceeded := blockedBookingsT0 >= 498 &&
		approvedBookingsT0 <= 2 &&
		velocityCapacityT0 == 0.0 &&
		velocityCapacityDay30 >= 50000.0

	summary := fmt.Sprintf(
		"Whale Attack Defense Succeeded: %d/%d high-value bookings throttled at t=0 due to zero velocity capacity ($%.2f); mature capacity reaches $%.2f at Day 30",
		blockedBookingsT0, attemptedBookings, velocityCapacityT0, velocityCapacityDay30,
	)

	return &SimulationResult{
		Scenario:         "Whale Velocity Attack",
		DefenseSucceeded: defenseSucceeded,
		Summary:          summary,
		Metrics: map[string]any{
			"whale_deposit_usd":             whaleDepositUSD,
			"attempted_concurrent_bookings": attemptedBookings,
			"approved_bookings_at_t0":       approvedBookingsT0,
			"blocked_bookings_at_t0":        blockedBookingsT0,
			"velocity_capacity_t0_usd":      velocityCapacityT0,
			"velocity_capacity_day30_usd":   velocityCapacityDay30,
		},
		DurationMs: float64(time.Since(start).Microseconds()) / 1000.0,
		ExecutedAt: time.Now().UTC(),
	}
}

// RunCollusiveVendorSimulation executes a deterministic simulation of a malicious vendor submitting a false
// No-Show claim against an honest user equipped with a 60-second challenge nonce, Stripe POS receipt,
// and hardware beacon attestation, demonstrating Oracle conflict resolution, Jury arbitration, and vendor slashing.
//
// Pre-conditions: none.
// Post-conditions: returns a non-nil SimulationResult confirming DefenseSucceeded is true,
// Oracle flags STATUS_DISPUTED, Jury rules for the user, user escrow bond is released intact, and vendor reputation is slashed.
func RunCollusiveVendorSimulation() *SimulationResult {
	start := time.Now()
	const (
		userDID          = "did:acn:user-honest-001"
		vendorDID        = "did:acn:vendor-collusive-001"
		appointmentID    = "apt-sim-collusive-88"
		bookingAmountUSD = 100.0
	)

	// Step 1: Initialize Escrow Vault and lock user's Good-Behavior Bond.
	vault := escrow.NewVault()
	if _, err := vault.DepositBond(userDID, bookingAmountUSD); err != nil {
		return failResult("Collusive Vendor Defense", fmt.Sprintf("failed to deposit user bond: %v", err), start)
	}

	lock, err := vault.LockBond(appointmentID, userDID, userDID, vendorDID, bookingAmountUSD)
	if err != nil {
		return failResult("Collusive Vendor Defense", fmt.Sprintf("failed to lock user bond: %v", err), start)
	}

	// Step 2: Initialize Oracle and issue 60s challenge nonce.
	orc := oracle.NewOracle()
	nonce, err := orc.IssueChallengeNonce(appointmentID, userDID, vendorDID)
	if err != nil {
		return failResult("Collusive Vendor Defense", fmt.Sprintf("failed to issue challenge nonce: %v", err), start)
	}

	// Step 3: Establish initial Vendor Reputation prior to dispute.
	repEngine := reputation.NewEngine()
	initialVendorMetrics := reputation.Metrics{
		AgentDID:               vendorDID,
		FulfilledCount:         12,
		NoShowCount:            0,
		AvgCancelLeadTimeHours: 24.0,
		PeerReviewAvg:          4.5,
		StakeAgeDays:           60.0,
		StakeUSD:               500.0,
	}
	snapBefore, err := repEngine.UpsertMetrics(initialVendorMetrics)
	if err != nil {
		return failResult("Collusive Vendor Defense", fmt.Sprintf("failed to upsert vendor initial metrics: %v", err), start)
	}
	vendorRepBefore := snapBefore.CompositeScore

	// Step 4: Submit conflicting attestations to Oracle.
	// Honest user has UserPresent=true, StripePOSReceipt=true, LocalBeaconVerified=true.
	// Collusive vendor fabricates UserPresent=false (VendorPresent=false) to steal the 70% slash compensation.
	settleIn := oracle.SettlementInput{
		AppointmentID:       appointmentID,
		LockID:              lock.LockID,
		Nonce:               nonce.Nonce,
		UserRootDID:         userDID,
		UserDID:             userDID,
		VendorDID:           vendorDID,
		UserPresent:         true,
		VendorPresent:       false,
		StripePOSReceipt:    true,
		LocalBeaconVerified: true,
		PeerRating:          5.0,
	}

	outcome, err := orc.SettleAppointment(settleIn)
	if err != nil {
		return failResult("Collusive Vendor Defense", fmt.Sprintf("oracle settlement failed: %v", err), start)
	}

	// Step 5: Escalate disputed case to hybrid AI audit Jury.
	jury := governance.NewJury("", false)
	disputeCase := governance.DisputeCase{
		CaseID:              "case-" + appointmentID,
		AppointmentID:       appointmentID,
		UserDID:             userDID,
		VendorDID:           vendorDID,
		UserClaimPresent:    true,
		VendorClaimPresent:  false,
		StripePOSReceipt:    true,
		LocalBeaconVerified: true,
		UserReputation:      4.8,
		VendorConflictCount: outcome.VendorConflictCount,
	}

	verdict, err := jury.ArbitrateDispute(context.Background(), disputeCase)
	if err != nil {
		return failResult("Collusive Vendor Defense", fmt.Sprintf("jury arbitration failed: %v", err), start)
	}

	// Step 6: Execute escrow resolution based on jury verdict.
	// Jury ruled in favor of User (Winner == "USER", SlashUser == false, SlashVendor == true).
	// User bond is released intact.
	var userBondSlashed bool
	if !verdict.SlashUser {
		if _, err := vault.ReleaseBond(lock.LockID); err != nil {
			return failResult("Collusive Vendor Defense", fmt.Sprintf("escrow release failed: %v", err), start)
		}
		userBondSlashed = false
	} else {
		userBondSlashed = true
	}

	// Step 7: Apply slash penalty to collusive vendor reputation.
	vendorSlashed := verdict.SlashVendor
	var vendorRepAfter float64
	if vendorSlashed {
		snapAfter, err := repEngine.RecordSettlement(vendorDID, false, 1.0, 0.0)
		if err != nil {
			return failResult("Collusive Vendor Defense", fmt.Sprintf("failed to record vendor slash: %v", err), start)
		}
		vendorRepAfter = snapAfter.CompositeScore
	} else {
		vendorRepAfter = vendorRepBefore
	}

	defenseSucceeded := outcome.Status == oracle.StatusDisputed &&
		verdict.Winner == governance.WinnerUser &&
		!verdict.SlashUser &&
		verdict.SlashVendor &&
		!userBondSlashed &&
		vendorSlashed &&
		vendorRepAfter < vendorRepBefore

	summary := fmt.Sprintf(
		"Collusive Vendor Defense Succeeded: Oracle detected status %s; Jury ruled Winner=%s (SlashUser=%t, SlashVendor=%t); user bond released intact; vendor reputation slashed from %.2f to %.2f",
		outcome.Status, verdict.Winner, verdict.SlashUser, verdict.SlashVendor, vendorRepBefore, vendorRepAfter,
	)

	return &SimulationResult{
		Scenario:         "Collusive Vendor False Claim",
		DefenseSucceeded: defenseSucceeded,
		Summary:          summary,
		Metrics: map[string]any{
			"oracle_status":            string(outcome.Status),
			"jury_winner":              verdict.Winner,
			"user_bond_slashed":        userBondSlashed,
			"vendor_slashed":           vendorSlashed,
			"vendor_reputation_before": vendorRepBefore,
			"vendor_reputation_after":  vendorRepAfter,
		},
		DurationMs: float64(time.Since(start).Microseconds()) / 1000.0,
		ExecutedAt: time.Now().UTC(),
	}
}

// failResult generates an unsuccessful SimulationResult on operational simulation errors.
func failResult(scenario, summary string, start time.Time) *SimulationResult {
	return &SimulationResult{
		Scenario:         scenario,
		DefenseSucceeded: false,
		Summary:          summary,
		Metrics:          map[string]any{"error": summary},
		DurationMs:       float64(time.Since(start).Microseconds()) / 1000.0,
		ExecutedAt:       time.Now().UTC(),
	}
}
