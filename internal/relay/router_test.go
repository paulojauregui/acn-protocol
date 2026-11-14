package relay

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/paulojauregui/acn-protocol/internal/escrow"
	"github.com/paulojauregui/acn-protocol/internal/identity"
	"github.com/paulojauregui/acn-protocol/internal/ledger"
	"github.com/paulojauregui/acn-protocol/internal/reputation"
)

// setupRouterFixture initializes an isolated suite of all four ACN protocol subsystems.
//
// Pre-conditions: none.
// Post-conditions: returns initialized Registry, Engine, Vault, Ledger, and Router instances.
func setupRouterFixture(t *testing.T) (*identity.Registry, *reputation.Engine, *escrow.Vault, *ledger.Ledger, *Router) {
	t.Helper()

	reg := identity.NewRegistry()
	rep := reputation.NewEngine()
	vault := escrow.NewVault()
	led := ledger.NewLedger()

	router := NewRouter(reg, rep, vault, led)
	return reg, rep, vault, led, router
}

// TestNegotiateBooking_HappyPath_SubAgent verifies the complete four-step state machine transition
// INITIATED -> REPUTATION_VERIFIED -> ESCROW_LOCKED -> CONFIRMED using a rotating Sub-Agent DID.
//
// Pre-conditions: registered root agent with bonded capital, active escrow deposit, and a freshly spawned sub-agent.
// Post-conditions: booking successfully confirmed; state history matches expected sequence; escrow lock created.
func TestNegotiateBooking_HappyPath_SubAgent(t *testing.T) {
	reg, _, vault, _, router := setupRouterFixture(t)

	// 1. Register caller Root Agent and fund with $100 bond to reach Tier 1 Economic
	callerRoot, err := reg.RegisterRootAgent("Autonomous Fleet Operator", nil, false, false, false)
	if err != nil {
		t.Fatalf("RegisterRootAgent caller failed: %v", err)
	}
	callerRoot, err = reg.UpdateEconomics(callerRoot.RootDID, 100.0, 50.0, 5, 3.8)
	if err != nil {
		t.Fatalf("UpdateEconomics caller failed: %v", err)
	}
	if callerRoot.TrustTier < identity.Tier1Economic {
		t.Fatalf("expected Tier1Economic or higher, got %s", callerRoot.TrustTier)
	}

	// 2. Deposit bond in escrow vault for caller Root Agent
	if _, err := vault.DepositBond(callerRoot.RootDID, 300.0); err != nil {
		t.Fatalf("DepositBond failed: %v", err)
	}

	// 3. Spawn a rotating, unlinkable Sub-Agent DID
	subAgent, err := reg.SpawnSubAgent(callerRoot.RootDID, "ride-hailing-dispatch", nil, 2*time.Hour)
	if err != nil {
		t.Fatalf("SpawnSubAgent failed: %v", err)
	}

	// 4. Register vendor Root Agent
	vendorRoot, err := reg.RegisterRootAgent("Waymo Fleet SF", nil, true, true, true)
	if err != nil {
		t.Fatalf("RegisterRootAgent vendor failed: %v", err)
	}

	// 5. Execute booking negotiation through rotating Sub-Agent
	req := BookingRequest{
		CallerDID:         subAgent.SubDID,
		VendorDID:         vendorRoot.RootDID,
		ServiceName:       "autonomous-taxi-sf",
		BondAmountUSD:     25.0,
		MinRequiredScore:  3.0,
		RequiredTrustTier: identity.Tier1Economic,
	}

	session, err := router.NegotiateBooking(req)
	if err != nil {
		t.Fatalf("NegotiateBooking failed: %v", err)
	}

	// 6. Validate session attributes and identity inheritance
	if session.State != StateConfirmed {
		t.Fatalf("expected state %s, got %s (rejection reason: %s)", StateConfirmed, session.State, session.RejectionReason)
	}
	if session.CallerDID != subAgent.SubDID {
		t.Fatalf("expected CallerDID %s, got %s", subAgent.SubDID, session.CallerDID)
	}
	if session.CallerRootDID != callerRoot.RootDID {
		t.Fatalf("expected CallerRootDID %s, got %s", callerRoot.RootDID, session.CallerRootDID)
	}
	if session.CallerTier != identity.Tier1Economic {
		t.Fatalf("expected CallerTier %s, got %s", identity.Tier1Economic, session.CallerTier)
	}
	if session.CallerScore != 3.8 {
		t.Fatalf("expected CallerScore 3.8, got %f", session.CallerScore)
	}
	if session.LockID == "" {
		t.Fatalf("expected non-empty LockID on confirmed session")
	}
	if session.PassportID == "" {
		t.Fatalf("expected non-empty PassportID on confirmed session")
	}
	if session.RejectionReason != "" {
		t.Fatalf("expected empty RejectionReason, got %s", session.RejectionReason)
	}

	// 7. Validate exact state machine transition sequence
	expectedHistory := []SessionState{
		StateInitiated,
		StateReputationVerified,
		StateEscrowLocked,
		StateConfirmed,
	}
	if len(session.StateHistory) != len(expectedHistory) {
		t.Fatalf("expected history length %d, got %d: %v", len(expectedHistory), len(session.StateHistory), session.StateHistory)
	}
	for i, state := range expectedHistory {
		if session.StateHistory[i] != state {
			t.Fatalf("history index %d mismatch: expected %s, got %s", i, state, session.StateHistory[i])
		}
	}

	// 8. Verify active lock state in escrow vault
	lock, err := vault.GetLock(session.LockID)
	if err != nil {
		t.Fatalf("GetLock failed: %v", err)
	}
	if lock.Status != escrow.LockStatusActive {
		t.Fatalf("expected lock status ACTIVE, got %s", lock.Status)
	}
	if lock.AmountUSD != 25.0 {
		t.Fatalf("expected lock amount 25.0, got %f", lock.AmountUSD)
	}
	if lock.PayerRootDID != callerRoot.RootDID {
		t.Fatalf("expected payer root DID %s, got %s", callerRoot.RootDID, lock.PayerRootDID)
	}

	// 9. Verify listing and retrieval
	sessions := router.ListSessions()
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session in ListSessions, got %d", len(sessions))
	}
	if sessions[0].SessionID != session.SessionID {
		t.Fatalf("session ID mismatch in ListSessions")
	}

	fetched, err := router.GetSession(session.SessionID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if fetched.SessionID != session.SessionID {
		t.Fatalf("GetSession mismatch: %+v vs %+v", fetched, session)
	}
}

// TestNegotiateBooking_PreAnswerRejections verifies gatekeeper rejections across all dimensions.
//
// Pre-conditions: router fixture configured with varying trust, tier, exposure, velocity, and escrow levels.
// Post-conditions: sessions transition to StateRejectedLowTrust with specific descriptive RejectionReason strings.
func TestNegotiateBooking_PreAnswerRejections(t *testing.T) {
	t.Run("ScoreBelowRequirement", func(t *testing.T) {
		reg, _, vault, _, router := setupRouterFixture(t)
		callerRoot, err := reg.RegisterRootAgent("Low Score Agent", nil, false, false, false)
		if err != nil {
			t.Fatalf("RegisterRootAgent failed: %v", err)
		}
		// Default score is 2.50
		if _, err := vault.DepositBond(callerRoot.RootDID, 100.0); err != nil {
			t.Fatalf("DepositBond failed: %v", err)
		}

		req := BookingRequest{
			CallerDID:         callerRoot.RootDID,
			VendorDID:         "did:acn:vendor1",
			ServiceName:       "premium-service",
			BondAmountUSD:     5.0,
			MinRequiredScore:  4.0,
			RequiredTrustTier: identity.Tier0Ephemeral,
		}

		session, err := router.NegotiateBooking(req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if session.State != StateRejectedLowTrust {
			t.Fatalf("expected %s, got %s", StateRejectedLowTrust, session.State)
		}
		if session.RejectionReason == "" {
			t.Fatalf("expected non-empty rejection reason")
		}
		expectedHistory := []SessionState{StateInitiated, StateRejectedLowTrust}
		if len(session.StateHistory) != len(expectedHistory) {
			t.Fatalf("unexpected state history length: %v", session.StateHistory)
		}
		for i, state := range expectedHistory {
			if session.StateHistory[i] != state {
				t.Fatalf("history index %d mismatch: expected %s, got %s", i, state, session.StateHistory[i])
			}
		}
	})

	t.Run("TrustTierBelowRequirement", func(t *testing.T) {
		reg, _, vault, _, router := setupRouterFixture(t)
		callerRoot, err := reg.RegisterRootAgent("Ephemeral Agent", nil, false, false, false)
		if err != nil {
			t.Fatalf("RegisterRootAgent failed: %v", err)
		}
		if _, err := vault.DepositBond(callerRoot.RootDID, 500.0); err != nil {
			t.Fatalf("DepositBond failed: %v", err)
		}

		req := BookingRequest{
			CallerDID:         callerRoot.RootDID,
			VendorDID:         "did:acn:vendor1",
			ServiceName:       "reputable-only-service",
			BondAmountUSD:     5.0,
			MinRequiredScore:  2.0,
			RequiredTrustTier: identity.Tier2Reputational, // Caller is Tier 0 Ephemeral
		}

		session, err := router.NegotiateBooking(req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if session.State != StateRejectedLowTrust {
			t.Fatalf("expected %s, got %s", StateRejectedLowTrust, session.State)
		}
	})

	t.Run("ExposureCeilingExceeded", func(t *testing.T) {
		reg, _, vault, _, router := setupRouterFixture(t)
		callerRoot, err := reg.RegisterRootAgent("Tier0 Agent", nil, false, false, false)
		if err != nil {
			t.Fatalf("RegisterRootAgent failed: %v", err)
		}
		if _, err := vault.DepositBond(callerRoot.RootDID, 1000.0); err != nil {
			t.Fatalf("DepositBond failed: %v", err)
		}

		// Tier 0 has max exposure of 5.00 USD. Request 10.00 USD.
		req := BookingRequest{
			CallerDID:         callerRoot.RootDID,
			VendorDID:         "did:acn:vendor1",
			ServiceName:       "heavy-task",
			BondAmountUSD:     10.0,
			MinRequiredScore:  2.0,
			RequiredTrustTier: identity.Tier0Ephemeral,
		}

		session, err := router.NegotiateBooking(req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if session.State != StateRejectedLowTrust {
			t.Fatalf("expected %s, got %s", StateRejectedLowTrust, session.State)
		}
	})

	t.Run("WhaleAttackZeroVelocityCapacity", func(t *testing.T) {
		reg, rep, vault, _, router := setupRouterFixture(t)
		callerRoot, err := reg.RegisterRootAgent("Whale Attacker", nil, false, false, false)
		if err != nil {
			t.Fatalf("RegisterRootAgent failed: %v", err)
		}
		// Promote root agent to Tier 1 via bond
		callerRoot, err = reg.UpdateEconomics(callerRoot.RootDID, 100.0, 0.0, 0, 3.0)
		if err != nil {
			t.Fatalf("UpdateEconomics failed: %v", err)
		}
		if _, err := vault.DepositBond(callerRoot.RootDID, 1000.0); err != nil {
			t.Fatalf("DepositBond failed: %v", err)
		}

		// Feed brand new whale metrics into reputation engine: high stake, but 0 age and 0 settled tx
		_, err = rep.UpsertMetrics(reputation.Metrics{
			AgentDID:       callerRoot.RootDID,
			StakeUSD:       100000.0,
			AccountAgeDays: 0,
			SettledTxCount: 0,
		})
		if err != nil {
			t.Fatalf("UpsertMetrics failed: %v", err)
		}

		req := BookingRequest{
			CallerDID:         callerRoot.RootDID,
			VendorDID:         "did:acn:vendor1",
			ServiceName:       "whale-target",
			BondAmountUSD:     25.0,
			MinRequiredScore:  2.0,
			RequiredTrustTier: identity.Tier1Economic,
		}

		session, err := router.NegotiateBooking(req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if session.State != StateRejectedLowTrust {
			t.Fatalf("expected whale rejection, got %s", session.State)
		}
		if session.RejectionReason != "zero velocity capacity for newly created unseasoned account" {
			t.Fatalf("unexpected rejection reason: %s", session.RejectionReason)
		}
	})

	t.Run("InsufficientEscrowBond", func(t *testing.T) {
		reg, _, vault, _, router := setupRouterFixture(t)
		callerRoot, err := reg.RegisterRootAgent("Broke Agent", nil, false, false, false)
		if err != nil {
			t.Fatalf("RegisterRootAgent failed: %v", err)
		}
		// Promote to Tier 1
		callerRoot, err = reg.UpdateEconomics(callerRoot.RootDID, 100.0, 50.0, 5, 3.5)
		if err != nil {
			t.Fatalf("UpdateEconomics failed: %v", err)
		}

		// Deposit only $10 in escrow vault
		if _, err := vault.DepositBond(callerRoot.RootDID, 10.0); err != nil {
			t.Fatalf("DepositBond failed: %v", err)
		}

		// Request booking requiring $50 bond
		req := BookingRequest{
			CallerDID:         callerRoot.RootDID,
			VendorDID:         "did:acn:vendor1",
			ServiceName:       "expensive-service",
			BondAmountUSD:     50.0,
			MinRequiredScore:  3.0,
			RequiredTrustTier: identity.Tier1Economic,
		}

		session, err := router.NegotiateBooking(req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if session.State != StateRejectedLowTrust {
			t.Fatalf("expected %s, got %s", StateRejectedLowTrust, session.State)
		}
		if session.RejectionReason != "insufficient available escrow bond" {
			t.Fatalf("expected exact rejection reason 'insufficient available escrow bond', got '%s'", session.RejectionReason)
		}
		// State history should show passport was verified, but escrow lock failed
		expectedHistory := []SessionState{StateInitiated, StateReputationVerified, StateRejectedLowTrust}
		if len(session.StateHistory) != len(expectedHistory) {
			t.Fatalf("expected history length %d, got %d: %v", len(expectedHistory), len(session.StateHistory), session.StateHistory)
		}
		for i, state := range expectedHistory {
			if session.StateHistory[i] != state {
				t.Fatalf("history index %d mismatch: expected %s, got %s", i, state, session.StateHistory[i])
			}
		}
	})
}

// TestNegotiateBooking_Concurrency validates thread safety under 32 parallel booking negotiations.
//
// Pre-conditions: 32 concurrent goroutines negotiating bookings simultaneously with unique root and sub agents.
// Post-conditions: execution completes without data races or lock contention under `go test -race`.
func TestNegotiateBooking_Concurrency(t *testing.T) {
	const numGoroutines = 32
	reg, _, vault, _, router := setupRouterFixture(t)

	vendorRoot, err := reg.RegisterRootAgent("Target Vendor", nil, true, true, true)
	if err != nil {
		t.Fatalf("RegisterRootAgent vendor failed: %v", err)
	}

	type callerFixture struct {
		subDID  string
		rootDID string
	}
	callers := make([]callerFixture, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		root, err := reg.RegisterRootAgent(fmt.Sprintf("Concurrent Root %d", i), nil, false, false, false)
		if err != nil {
			t.Fatalf("setup root failed: %v", err)
		}
		root, err = reg.UpdateEconomics(root.RootDID, 100.0, 50.0, 10, 4.0)
		if err != nil {
			t.Fatalf("setup economics failed: %v", err)
		}
		if _, err := vault.DepositBond(root.RootDID, 500.0); err != nil {
			t.Fatalf("setup deposit failed: %v", err)
		}
		sub, err := reg.SpawnSubAgent(root.RootDID, fmt.Sprintf("worker-%d", i), nil, time.Hour)
		if err != nil {
			t.Fatalf("setup subagent failed: %v", err)
		}
		callers[i] = callerFixture{
			subDID:  sub.SubDID,
			rootDID: root.RootDID,
		}
	}

	var wg sync.WaitGroup
	wg.Add(numGoroutines)
	errorsChan := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		workerID := i
		go func() {
			defer wg.Done()
			cf := callers[workerID]

			req := BookingRequest{
				CallerDID:         cf.subDID,
				VendorDID:         vendorRoot.RootDID,
				ServiceName:       fmt.Sprintf("service-%d", workerID),
				BondAmountUSD:     15.0,
				MinRequiredScore:  3.5,
				RequiredTrustTier: identity.Tier1Economic,
			}

			session, err := router.NegotiateBooking(req)
			if err != nil {
				errorsChan <- fmt.Errorf("worker %d error: %w", workerID, err)
				return
			}
			if session.State != StateConfirmed {
				errorsChan <- fmt.Errorf("worker %d unconfirmed state: %s (reason: %s)", workerID, session.State, session.RejectionReason)
				return
			}
			if session.CallerRootDID != cf.rootDID {
				errorsChan <- fmt.Errorf("worker %d root did mismatch: %s != %s", workerID, session.CallerRootDID, cf.rootDID)
				return
			}
		}()
	}

	wg.Wait()
	close(errorsChan)

	for err := range errorsChan {
		t.Fatalf("concurrent negotiation failure: %v", err)
	}

	allSessions := router.ListSessions()
	if len(allSessions) != numGoroutines {
		t.Fatalf("expected %d sessions recorded, got %d", numGoroutines, len(allSessions))
	}
}

// TestNegotiateBooking_ValidationErrors verifies input validation and missing dependency handling.
//
// Pre-conditions: invalid inputs or unconfigured Router instances.
// Post-conditions: returns expected sentinel errors without mutating state.
func TestNegotiateBooking_ValidationErrors(t *testing.T) {
	_, rep, vault, led, router := setupRouterFixture(t)

	// Nil dependency check
	badRouter := NewRouter(nil, rep, vault, led)
	if _, err := badRouter.NegotiateBooking(BookingRequest{CallerDID: "c", VendorDID: "v"}); !errors.Is(err, ErrNilDependency) {
		t.Fatalf("expected ErrNilDependency, got %v", err)
	}

	// Empty CallerDID
	if _, err := router.NegotiateBooking(BookingRequest{CallerDID: "", VendorDID: "v"}); !errors.Is(err, ErrInvalidCallerDID) {
		t.Fatalf("expected ErrInvalidCallerDID, got %v", err)
	}

	// Empty VendorDID
	if _, err := router.NegotiateBooking(BookingRequest{CallerDID: "c", VendorDID: ""}); !errors.Is(err, ErrInvalidVendorDID) {
		t.Fatalf("expected ErrInvalidVendorDID, got %v", err)
	}

	// Unknown CallerDID
	sess, err := router.NegotiateBooking(BookingRequest{CallerDID: "did:acn:unknown123", VendorDID: "did:acn:vendor123"})
	if err != nil {
		t.Fatalf("unexpected error for unknown caller DID: %v", err)
	}
	if sess.State != StateRejectedLowTrust {
		t.Fatalf("expected %s for unknown caller, got %s", StateRejectedLowTrust, sess.State)
	}

	// Unknown session lookup
	if _, err := router.GetSession(""); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound for empty session ID, got %v", err)
	}
	if _, err := router.GetSession("sess_nonexistent"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound for unknown session ID, got %v", err)
	}
}
