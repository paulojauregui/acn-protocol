package oracle

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestIssueChallengeNonce(t *testing.T) {
	o := NewOracle()

	// Empty input validation
	if _, err := o.IssueChallengeNonce("", "did:acn:root:user1", "did:acn:root:vendor1"); !errors.Is(err, ErrEmptyAppointmentID) {
		t.Fatalf("expected ErrEmptyAppointmentID, got %v", err)
	}
	if _, err := o.IssueChallengeNonce("apt-1", "", "did:acn:root:vendor1"); !errors.Is(err, ErrEmptyUserDID) {
		t.Fatalf("expected ErrEmptyUserDID, got %v", err)
	}
	if _, err := o.IssueChallengeNonce("apt-1", "did:acn:root:user1", ""); !errors.Is(err, ErrEmptyVendorDID) {
		t.Fatalf("expected ErrEmptyVendorDID, got %v", err)
	}

	// Valid issuance
	cn, err := o.IssueChallengeNonce("apt-1", "did:acn:root:user1", "did:acn:root:vendor1")
	if err != nil {
		t.Fatalf("unexpected error issuing nonce: %v", err)
	}
	if len(cn.Nonce) != 32 {
		t.Fatalf("expected 32-char hex nonce, got length %d (%s)", len(cn.Nonce), cn.Nonce)
	}
	if cn.AppointmentID != "apt-1" {
		t.Fatalf("expected appointment ID apt-1, got %s", cn.AppointmentID)
	}
	if cn.Consumed {
		t.Fatalf("expected fresh nonce to be unconsumed")
	}
	ttl := cn.ExpiresAt.Sub(cn.IssuedAt)
	if ttl != DefaultNonceTTL {
		t.Fatalf("expected TTL of %v, got %v", DefaultNonceTTL, ttl)
	}

	// Verify deep copy does not mutate internal registry
	cn.Consumed = true
	in := SettlementInput{
		AppointmentID: "apt-1",
		Nonce:         cn.Nonce,
		VendorDID:     "did:acn:root:vendor1",
		UserPresent:   true,
		VendorPresent: true,
	}
	outcome, err := o.SettleAppointment(in)
	if err != nil {
		t.Fatalf("expected settlement to succeed despite caller mutating returned copy: %v", err)
	}
	if outcome.Status != StatusSuccess {
		t.Fatalf("expected StatusSuccess, got %v", outcome.Status)
	}
}

func TestHandshakeSignature(t *testing.T) {
	secret := "test-secret-key-0123456789"
	aptID := "apt-100"
	nonce := "0123456789abcdef0123456789abcdef"
	signer := "did:acn:root:agent42"

	sigTrue := ComputePresenceSignature(secret, aptID, nonce, signer, true)
	if sigTrue == "" {
		t.Fatalf("expected non-empty hex signature")
	}

	// Determinism
	sigTrue2 := ComputePresenceSignature(secret, aptID, nonce, signer, true)
	if sigTrue != sigTrue2 {
		t.Fatalf("expected deterministic signatures, got %s vs %s", sigTrue, sigTrue2)
	}

	// Different payload -> different signature
	sigFalse := ComputePresenceSignature(secret, aptID, nonce, signer, false)
	if sigTrue == sigFalse {
		t.Fatalf("expected different signature when presence changes")
	}

	// Verify valid token
	token := SignedPresenceToken{
		AppointmentID: aptID,
		Nonce:         nonce,
		SignerDID:     signer,
		Present:       true,
		Signature:     sigTrue,
		Timestamp:     time.Now(),
	}
	if !VerifyPresenceSignature(secret, token) {
		t.Fatalf("expected valid signature verification to succeed")
	}

	// Verify wrong secret fails
	if VerifyPresenceSignature("wrong-secret", token) {
		t.Fatalf("expected verification with wrong secret to fail")
	}

	// Verify empty secret or signature fails
	if VerifyPresenceSignature("", token) {
		t.Fatalf("expected verification with empty secret to fail")
	}
	emptySigToken := token
	emptySigToken.Signature = ""
	if VerifyPresenceSignature(secret, emptySigToken) {
		t.Fatalf("expected verification with empty signature to fail")
	}

	// Verify tampered signature fails
	tamperedToken := token
	tamperedToken.Signature = "abcdef01"
	if VerifyPresenceSignature(secret, tamperedToken) {
		t.Fatalf("expected verification with tampered signature to fail")
	}

	// Verify non-hex signature fails safely
	malformedToken := token
	malformedToken.Signature = "not-a-hex-signature-string!!"
	if VerifyPresenceSignature(secret, malformedToken) {
		t.Fatalf("expected verification with malformed signature to fail")
	}
}

func TestSettleAppointment_Success(t *testing.T) {
	o := NewOracle()
	userDID := "did:acn:sub:user123"
	vendorDID := "did:acn:root:barbershop"
	aptID := "apt-booking-001"

	cn, err := o.IssueChallengeNonce(aptID, userDID, vendorDID)
	if err != nil {
		t.Fatalf("failed to issue challenge nonce: %v", err)
	}

	in := SettlementInput{
		AppointmentID:       aptID,
		LockID:              "lock-bond-99",
		Nonce:               cn.Nonce,
		UserRootDID:         "did:acn:root:user123",
		UserDID:             userDID,
		VendorDID:           vendorDID,
		UserPresent:         true,
		VendorPresent:       true,
		StripePOSReceipt:    true,
		LocalBeaconVerified: true,
		PeerRating:          5.0,
	}

	outcome, err := o.SettleAppointment(in)
	if err != nil {
		t.Fatalf("settlement failed: %v", err)
	}

	if outcome.Status != StatusSuccess {
		t.Fatalf("expected StatusSuccess, got %v", outcome.Status)
	}
	if outcome.UserSlashed || outcome.VendorSlashed {
		t.Fatalf("neither party should be slashed on successful check-in")
	}
	if outcome.HonestReporterRewardUSD != VendorHonestReporterRewardUSD {
		t.Fatalf("expected reward %v, got %v", VendorHonestReporterRewardUSD, outcome.HonestReporterRewardUSD)
	}
	if outcome.VendorConflictCount != 0 {
		t.Fatalf("expected 0 vendor conflicts, got %d", outcome.VendorConflictCount)
	}

	// Verify GetOutcome retrieval
	fetched, err := o.GetOutcome(aptID)
	if err != nil {
		t.Fatalf("failed to get outcome: %v", err)
	}
	if fetched.Status != StatusSuccess || fetched.AppointmentID != aptID {
		t.Fatalf("retrieved outcome mismatch: %+v", fetched)
	}
}

func TestSettleAppointment_ReplayRejection(t *testing.T) {
	o := NewOracle()
	userDID := "did:acn:sub:user4"
	vendorDID := "did:acn:root:vendor4"
	aptID := "apt-replay-001"

	cn, err := o.IssueChallengeNonce(aptID, userDID, vendorDID)
	if err != nil {
		t.Fatalf("failed to issue nonce: %v", err)
	}

	in := SettlementInput{
		AppointmentID: aptID,
		Nonce:         cn.Nonce,
		VendorDID:     vendorDID,
		UserPresent:   true,
		VendorPresent: true,
	}

	// First settlement succeeds
	_, err = o.SettleAppointment(in)
	if err != nil {
		t.Fatalf("first settlement failed: %v", err)
	}

	// Second settlement with same appointment fails with ErrAlreadySettled
	_, err = o.SettleAppointment(in)
	if !errors.Is(err, ErrAlreadySettled) {
		t.Fatalf("expected ErrAlreadySettled on replay, got %v", err)
	}

	// Replay with different appointment but reused consumed nonce fails with ErrNonceAppointmentMismatch
	inReused := SettlementInput{
		AppointmentID: "apt-replay-002",
		Nonce:         cn.Nonce,
		VendorDID:     vendorDID,
		UserPresent:   true,
		VendorPresent: true,
	}
	_, err = o.SettleAppointment(inReused)
	if !errors.Is(err, ErrNonceAppointmentMismatch) {
		t.Fatalf("expected ErrNonceAppointmentMismatch, got %v", err)
	}
}

func TestSettleAppointment_ExpirationRejection(t *testing.T) {
	o := NewOracle()
	userDID := "did:acn:sub:user5"
	vendorDID := "did:acn:root:vendor5"
	aptID := "apt-exp-001"

	cn, err := o.IssueChallengeNonce(aptID, userDID, vendorDID)
	if err != nil {
		t.Fatalf("failed to issue nonce: %v", err)
	}

	// Artificially expire nonce using the test helper
	o.ExpireNonceForTest(cn.Nonce)

	in := SettlementInput{
		AppointmentID: aptID,
		Nonce:         cn.Nonce,
		VendorDID:     vendorDID,
		UserPresent:   true,
		VendorPresent: true,
	}

	_, err = o.SettleAppointment(in)
	if !errors.Is(err, ErrNonceExpired) {
		t.Fatalf("expected ErrNonceExpired, got %v", err)
	}
}

func TestSettleAppointment_MutualNoShow_StatusSlashed(t *testing.T) {
	o := NewOracle()
	userDID := "did:acn:sub:user6"
	vendorDID := "did:acn:root:vendor6"
	aptID := "apt-noshow-001"

	cn, err := o.IssueChallengeNonce(aptID, userDID, vendorDID)
	if err != nil {
		t.Fatalf("failed to issue nonce: %v", err)
	}

	in := SettlementInput{
		AppointmentID: aptID,
		Nonce:         cn.Nonce,
		VendorDID:     vendorDID,
		UserPresent:   false,
		VendorPresent: false,
	}

	outcome, err := o.SettleAppointment(in)
	if err != nil {
		t.Fatalf("settlement failed: %v", err)
	}

	if outcome.Status != StatusSlashed {
		t.Fatalf("expected StatusSlashed, got %v", outcome.Status)
	}
	if !outcome.UserSlashed {
		t.Fatalf("expected UserSlashed to be true")
	}
	if outcome.VendorSlashed {
		t.Fatalf("expected VendorSlashed to be false")
	}
	if outcome.HonestReporterRewardUSD != 0.0 {
		t.Fatalf("expected 0 reward for no-show, got %v", outcome.HonestReporterRewardUSD)
	}
}

func TestSettleAppointment_Dispute_UserCorroborated(t *testing.T) {
	o := NewOracle()
	userDID := "did:acn:sub:user7"
	vendorDID := "did:acn:root:vendor7"
	aptID := "apt-dispute-001"

	cn, err := o.IssueChallengeNonce(aptID, userDID, vendorDID)
	if err != nil {
		t.Fatalf("failed to issue nonce: %v", err)
	}

	// User claims present; vendor falsely claims absent; user has beacon AND POS receipt proof
	in := SettlementInput{
		AppointmentID:       aptID,
		Nonce:               cn.Nonce,
		VendorDID:           vendorDID,
		UserPresent:         true,
		VendorPresent:       false,
		StripePOSReceipt:    true,
		LocalBeaconVerified: true,
	}

	outcome, err := o.SettleAppointment(in)
	if err != nil {
		t.Fatalf("settlement failed: %v", err)
	}

	if outcome.Status != StatusDisputed {
		t.Fatalf("expected StatusDisputed, got %v", outcome.Status)
	}
	if outcome.UserSlashed {
		t.Fatalf("corroborated user should not be slashed")
	}
	if !outcome.VendorSlashed {
		t.Fatalf("fabricating vendor should be slashed")
	}
	if outcome.VendorConflictCount != 1 {
		t.Fatalf("expected vendor conflict count 1, got %d", outcome.VendorConflictCount)
	}
	if o.GetVendorConflictCount(vendorDID) != 1 {
		t.Fatalf("expected tracked conflict count 1, got %d", o.GetVendorConflictCount(vendorDID))
	}
}

func TestSettleAppointment_Dispute_UserUncorroborated(t *testing.T) {
	o := NewOracle()
	userDID := "did:acn:sub:user8"
	vendorDID := "did:acn:root:vendor8"
	aptID := "apt-dispute-002"

	cn, err := o.IssueChallengeNonce(aptID, userDID, vendorDID)
	if err != nil {
		t.Fatalf("failed to issue nonce: %v", err)
	}

	// User claims present, vendor claims absent, NO hardware or POS corroboration
	in := SettlementInput{
		AppointmentID:       aptID,
		Nonce:               cn.Nonce,
		VendorDID:           vendorDID,
		UserPresent:         true,
		VendorPresent:       false,
		StripePOSReceipt:    false,
		LocalBeaconVerified: false,
	}

	outcome, err := o.SettleAppointment(in)
	if err != nil {
		t.Fatalf("settlement failed: %v", err)
	}

	if outcome.Status != StatusDisputed {
		t.Fatalf("expected StatusDisputed, got %v", outcome.Status)
	}
	if !outcome.UserSlashed {
		t.Fatalf("uncorroborated user should be slashed")
	}
	if outcome.VendorSlashed {
		t.Fatalf("vendor should not be slashed when user lacks corroboration")
	}
	if outcome.VendorConflictCount != 1 {
		t.Fatalf("expected vendor conflict count 1, got %d", outcome.VendorConflictCount)
	}
}

func TestSettleAppointment_Dispute_VendorSlashThreshold(t *testing.T) {
	o := NewOracle()
	userDID := "did:acn:sub:user9"
	vendorDID := "did:acn:root:habitual-bad-vendor"

	// Create 3 disputes in sequence to hit VendorConflictSlashThreshold
	for i := 1; i <= VendorConflictSlashThreshold; i++ {
		aptID := fmt.Sprintf("apt-threshold-%03d", i)
		cn, err := o.IssueChallengeNonce(aptID, userDID, vendorDID)
		if err != nil {
			t.Fatalf("failed to issue nonce %d: %v", i, err)
		}

		in := SettlementInput{
			AppointmentID:       aptID,
			Nonce:               cn.Nonce,
			VendorDID:           vendorDID,
			UserPresent:         true,
			VendorPresent:       false,
			StripePOSReceipt:    false,
			LocalBeaconVerified: false,
		}

		outcome, err := o.SettleAppointment(in)
		if err != nil {
			t.Fatalf("settlement %d failed: %v", i, err)
		}

		if i < VendorConflictSlashThreshold {
			if !outcome.UserSlashed || outcome.VendorSlashed {
				t.Fatalf("round %d: expected user slashed before threshold reached", i)
			}
		} else {
			// At threshold, vendor is slashed!
			if outcome.UserSlashed {
				t.Fatalf("at threshold: user should not be slashed")
			}
			if !outcome.VendorSlashed {
				t.Fatalf("at threshold: habitual conflicting vendor must be slashed")
			}
		}
	}

	if o.GetVendorConflictCount(vendorDID) != VendorConflictSlashThreshold {
		t.Fatalf("expected conflict count %d, got %d", VendorConflictSlashThreshold, o.GetVendorConflictCount(vendorDID))
	}
}

func TestSettleAppointment_Dispute_UserAbsent_VendorPresent(t *testing.T) {
	o := NewOracle()
	userDID := "did:acn:sub:user10"
	vendorDID := "did:acn:root:vendor10"
	aptID := "apt-dispute-003"

	cn, err := o.IssueChallengeNonce(aptID, userDID, vendorDID)
	if err != nil {
		t.Fatalf("failed to issue nonce: %v", err)
	}

	in := SettlementInput{
		AppointmentID: aptID,
		Nonce:         cn.Nonce,
		VendorDID:     vendorDID,
		UserPresent:   false,
		VendorPresent: true,
	}

	outcome, err := o.SettleAppointment(in)
	if err != nil {
		t.Fatalf("settlement failed: %v", err)
	}

	if outcome.Status != StatusDisputed {
		t.Fatalf("expected StatusDisputed, got %v", outcome.Status)
	}
	if !outcome.UserSlashed {
		t.Fatalf("user who is absent while vendor present should be slashed")
	}
	if outcome.VendorSlashed {
		t.Fatalf("vendor should not be slashed")
	}
}

func TestOracle_GetOutcome_And_ListOutcomes(t *testing.T) {
	o := NewOracle()

	// Empty appointment ID check
	if _, err := o.GetOutcome(""); !errors.Is(err, ErrEmptyAppointmentID) {
		t.Fatalf("expected ErrEmptyAppointmentID, got %v", err)
	}

	// Unknown outcome check
	if _, err := o.GetOutcome("unknown-apt"); !errors.Is(err, ErrOutcomeNotFound) {
		t.Fatalf("expected ErrOutcomeNotFound, got %v", err)
	}

	// Empty list check
	if len(o.ListOutcomes()) != 0 {
		t.Fatalf("expected 0 outcomes initially")
	}

	// Settle 3 appointments in arbitrary order
	ids := []string{"apt-003", "apt-001", "apt-002"}
	for _, id := range ids {
		cn, err := o.IssueChallengeNonce(id, "did:acn:user", "did:acn:vendor")
		if err != nil {
			t.Fatalf("failed to issue nonce: %v", err)
		}
		_, err = o.SettleAppointment(SettlementInput{
			AppointmentID: id,
			Nonce:         cn.Nonce,
			VendorDID:     "did:acn:vendor",
			UserPresent:   true,
			VendorPresent: true,
		})
		if err != nil {
			t.Fatalf("settle %s failed: %v", id, err)
		}
	}

	// Verify ListOutcomes returns sorted list
	list := o.ListOutcomes()
	if len(list) != 3 {
		t.Fatalf("expected 3 outcomes, got %d", len(list))
	}
	if list[0].AppointmentID != "apt-001" || list[1].AppointmentID != "apt-002" || list[2].AppointmentID != "apt-003" {
		t.Fatalf("expected sorted outcomes [apt-001, apt-002, apt-003], got [%s, %s, %s]",
			list[0].AppointmentID, list[1].AppointmentID, list[2].AppointmentID)
	}

	// Verify deep copy isolation
	list[0].Status = StatusSlashed
	original, err := o.GetOutcome("apt-001")
	if err != nil {
		t.Fatalf("failed to get apt-001: %v", err)
	}
	if original.Status != StatusSuccess {
		t.Fatalf("modifying returned list element mutated internal state")
	}
}

func TestOracle_InputValidation(t *testing.T) {
	o := NewOracle()

	// Empty appointment ID
	if _, err := o.SettleAppointment(SettlementInput{AppointmentID: ""}); !errors.Is(err, ErrEmptyAppointmentID) {
		t.Fatalf("expected ErrEmptyAppointmentID, got %v", err)
	}

	// Empty nonce
	if _, err := o.SettleAppointment(SettlementInput{AppointmentID: "apt-1", Nonce: ""}); !errors.Is(err, ErrNonceNotFound) {
		t.Fatalf("expected ErrNonceNotFound, got %v", err)
	}

	// Empty vendor DID
	if _, err := o.SettleAppointment(SettlementInput{AppointmentID: "apt-1", Nonce: "nonce", VendorDID: ""}); !errors.Is(err, ErrEmptyVendorDID) {
		t.Fatalf("expected ErrEmptyVendorDID, got %v", err)
	}

	// Nonce not found
	if _, err := o.SettleAppointment(SettlementInput{AppointmentID: "apt-1", Nonce: "non-existent", VendorDID: "vendor"}); !errors.Is(err, ErrNonceNotFound) {
		t.Fatalf("expected ErrNonceNotFound, got %v", err)
	}
}

func TestOracle_Concurrency_Race(t *testing.T) {
	o := NewOracle()
	const workerCount = 20
	const iterations = 50

	var wg sync.WaitGroup
	wg.Add(workerCount)

	for w := 0; w < workerCount; w++ {
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				aptID := fmt.Sprintf("apt-w%d-i%d", workerID, i)
				userDID := fmt.Sprintf("did:acn:user:%d", workerID)
				vendorDID := fmt.Sprintf("did:acn:vendor:%d", workerID%4)

				cn, err := o.IssueChallengeNonce(aptID, userDID, vendorDID)
				if err != nil {
					t.Errorf("worker %d issue failed: %v", workerID, err)
					return
				}

				if i%5 == 0 {
					o.ExpireNonceForTest(cn.Nonce)
					_, err = o.SettleAppointment(SettlementInput{
						AppointmentID: aptID,
						Nonce:         cn.Nonce,
						VendorDID:     vendorDID,
						UserPresent:   true,
						VendorPresent: true,
					})
					if !errors.Is(err, ErrNonceExpired) {
						t.Errorf("expected ErrNonceExpired, got %v", err)
					}
				} else {
					outcome, err := o.SettleAppointment(SettlementInput{
						AppointmentID:       aptID,
						Nonce:               cn.Nonce,
						VendorDID:           vendorDID,
						UserPresent:         true,
						VendorPresent:       i%2 == 0,
						StripePOSReceipt:    true,
						LocalBeaconVerified: true,
					})
					if err != nil {
						t.Errorf("worker %d settle failed: %v", workerID, err)
						return
					}
					if outcome == nil {
						t.Errorf("nil outcome returned")
					}

					_, _ = o.GetOutcome(aptID)
					_ = o.GetVendorConflictCount(vendorDID)
				}

				if i%10 == 0 {
					_ = o.ListOutcomes()
				}
			}
		}(w)
	}

	wg.Wait()
}
