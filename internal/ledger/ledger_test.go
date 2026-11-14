package ledger_test

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/paulojauregui/acn-protocol/internal/ledger"
)

func TestHashLeaf_Deterministic(t *testing.T) {
	t.Parallel()

	h1 := ledger.HashLeaf("test-data-1")
	h2 := ledger.HashLeaf("test-data-1")
	h3 := ledger.HashLeaf("test-data-2")

	if h1 != h2 {
		t.Fatalf("expected deterministic HashLeaf, got %s != %s", h1, h2)
	}
	if h1 == h3 {
		t.Fatalf("expected different hashes for different data, got %s == %s", h1, h3)
	}
	if len(h1) != 64 {
		t.Fatalf("expected 64 hex characters, got %d", len(h1))
	}
}

func TestHashNodes_DeterministicAndOrderSensitive(t *testing.T) {
	t.Parallel()

	left := ledger.HashLeaf("left")
	right := ledger.HashLeaf("right")

	p1 := ledger.HashNodes(left, right)
	p2 := ledger.HashNodes(left, right)
	p3 := ledger.HashNodes(right, left)

	if p1 != p2 {
		t.Fatalf("expected deterministic HashNodes, got %s != %s", p1, p2)
	}
	if p1 == p3 {
		t.Fatalf("expected order-sensitive HashNodes, got %s == %s", p1, p3)
	}
	if len(p1) != 64 {
		t.Fatalf("expected 64 hex characters, got %d", len(p1))
	}
}

func TestComputeMerkleRoot_Empty(t *testing.T) {
	t.Parallel()

	root := ledger.ComputeMerkleRoot(nil)
	if len(root) != 64 {
		t.Fatalf("expected 64 hex characters for empty root, got %s", root)
	}

	rootEmptySlice := ledger.ComputeMerkleRoot([]string{})
	if root != rootEmptySlice {
		t.Fatalf("expected nil and empty slice to yield same root, got %s != %s", root, rootEmptySlice)
	}
}

func TestMerkleTree_InclusionProofAndVerification(t *testing.T) {
	t.Parallel()

	leafCounts := []int{1, 2, 3, 5, 8, 16}

	for _, count := range leafCounts {
		count := count
		t.Run(fmt.Sprintf("%d_Leaves", count), func(t *testing.T) {
			t.Parallel()

			leaves := make([]string, count)
			for i := 0; i < count; i++ {
				leaves[i] = ledger.HashLeaf(fmt.Sprintf("item-%d", i))
			}

			root := ledger.ComputeMerkleRoot(leaves)
			if len(root) != 64 {
				t.Fatalf("invalid Merkle root length: %d", len(root))
			}

			for i := 0; i < count; i++ {
				proof, err := ledger.GenerateInclusionProof(leaves, i)
				if err != nil {
					t.Fatalf("failed to generate proof for leaf %d of %d: %v", i, count, err)
				}
				if proof.LeafIndex != i {
					t.Fatalf("expected proof leaf index %d, got %d", i, proof.LeafIndex)
				}
				if proof.LeafHash != leaves[i] {
					t.Fatalf("expected proof leaf hash %s, got %s", leaves[i], proof.LeafHash)
				}
				if proof.MerkleRoot != root {
					t.Fatalf("expected proof root %s, got %s", root, proof.MerkleRoot)
				}

				if !ledger.VerifyInclusionProof(proof) {
					t.Fatalf("verification failed for valid proof on leaf %d of %d", i, count)
				}
			}
		})
	}
}

func TestMerkleTree_TamperRejection(t *testing.T) {
	t.Parallel()

	counts := []int{5, 16}
	for _, count := range counts {
		count := count
		t.Run(fmt.Sprintf("%d_Leaves_Tamper", count), func(t *testing.T) {
			t.Parallel()

			leaves := make([]string, count)
			for i := 0; i < count; i++ {
				leaves[i] = ledger.HashLeaf(fmt.Sprintf("item-%d", i))
			}

			targetIdx := count / 2
			proof, err := ledger.GenerateInclusionProof(leaves, targetIdx)
			if err != nil {
				t.Fatalf("failed to generate inclusion proof: %v", err)
			}

			// Base proof must succeed
			if !ledger.VerifyInclusionProof(proof) {
				t.Fatalf("expected valid proof to pass verification")
			}

			// Tamper 1: modify leaf hash
			tamperedLeaf := *proof
			tamperedLeaf.LeafHash = ledger.HashLeaf("corrupted-leaf")
			if ledger.VerifyInclusionProof(&tamperedLeaf) {
				t.Fatalf("expected tampered leaf hash to be rejected")
			}

			// Tamper 2: modify Merkle root
			tamperedRoot := *proof
			tamperedRoot.MerkleRoot = ledger.HashLeaf("corrupted-root")
			if ledger.VerifyInclusionProof(&tamperedRoot) {
				t.Fatalf("expected tampered Merkle root to be rejected")
			}

			// Tamper 3: modify a proof step sibling hash
			if len(proof.Steps) > 0 {
				tamperedStep := *proof
				tamperedStep.Steps = make([]ledger.ProofStep, len(proof.Steps))
				copy(tamperedStep.Steps, proof.Steps)
				tamperedStep.Steps[0].SiblingHash = ledger.HashLeaf("tampered-sibling")
				if ledger.VerifyInclusionProof(&tamperedStep) {
					t.Fatalf("expected tampered sibling step to be rejected")
				}

				// Tamper 4: invert IsLeft flag
				tamperedIsLeft := *proof
				tamperedIsLeft.Steps = make([]ledger.ProofStep, len(proof.Steps))
				copy(tamperedIsLeft.Steps, proof.Steps)
				tamperedIsLeft.Steps[0].IsLeft = !tamperedIsLeft.Steps[0].IsLeft
				if ledger.VerifyInclusionProof(&tamperedIsLeft) {
					t.Fatalf("expected inverted IsLeft flag to be rejected")
				}
			}

			// Tamper 5: empty fields
			emptyProof := *proof
			emptyProof.LeafHash = ""
			if ledger.VerifyInclusionProof(&emptyProof) {
				t.Fatalf("expected empty leaf hash to be rejected")
			}

			emptyRootProof := *proof
			emptyRootProof.MerkleRoot = ""
			if ledger.VerifyInclusionProof(&emptyRootProof) {
				t.Fatalf("expected empty root to be rejected")
			}

			// Tamper 6: nil proof
			if ledger.VerifyInclusionProof(nil) {
				t.Fatalf("expected nil proof to be rejected")
			}
		})
	}
}

func TestGenerateInclusionProof_BoundsChecking(t *testing.T) {
	t.Parallel()

	// Empty tree
	_, err := ledger.GenerateInclusionProof(nil, 0)
	if err == nil {
		t.Fatalf("expected error generating proof from empty tree")
	}

	leaves := []string{
		ledger.HashLeaf("a"),
		ledger.HashLeaf("b"),
	}

	// Negative index
	_, err = ledger.GenerateInclusionProof(leaves, -1)
	if err == nil {
		t.Fatalf("expected error on negative index")
	}

	// Index equal to length
	_, err = ledger.GenerateInclusionProof(leaves, 2)
	if err == nil {
		t.Fatalf("expected error on out-of-bounds index")
	}

	// Index far beyond length
	_, err = ledger.GenerateInclusionProof(leaves, 99)
	if err == nil {
		t.Fatalf("expected error on large out-of-bounds index")
	}
}

func TestLedger_AppendAndListRecords(t *testing.T) {
	t.Parallel()

	l := ledger.NewLedger()
	initialRoot := l.CurrentMerkleRoot()
	if initialRoot != ledger.ComputeMerkleRoot(nil) {
		t.Fatalf("expected initial root to match empty tree root")
	}

	recs := []ledger.InteractionRecord{
		{
			AppointmentID: "apt-101",
			UserRootDID:   "did:key:user1",
			SubAgentDID:   "did:key:sub1",
			VendorDID:     "did:key:vendor1",
			Status:        "settled",
			AmountUSD:     150.50,
		},
		{
			AppointmentID: "apt-102",
			UserRootDID:   "did:key:user2",
			SubAgentDID:   "did:key:sub2",
			VendorDID:     "did:key:vendor2",
			Status:        "settled",
			AmountUSD:     75.25,
		},
		{
			AppointmentID: "apt-103",
			UserRootDID:   "did:key:user3",
			SubAgentDID:   "did:key:sub3",
			VendorDID:     "did:key:vendor3",
			Status:        "settled",
			AmountUSD:     320.00,
		},
	}

	for i, r := range recs {
		appended, root, err := l.AppendRecord(r)
		if err != nil {
			t.Fatalf("failed to append record %d: %v", i, err)
		}
		if appended.RecordID == "" {
			t.Fatalf("expected non-empty RecordID")
		}
		if appended.LeafHash == "" {
			t.Fatalf("expected non-empty LeafHash")
		}
		if root != l.CurrentMerkleRoot() {
			t.Fatalf("expected returned root to match CurrentMerkleRoot")
		}
	}

	stored := l.ListRecords()
	if len(stored) != len(recs) {
		t.Fatalf("expected %d stored records, got %d", len(recs), len(stored))
	}

	// Defensive copy check: modifying returned slice must not affect internal state
	stored[0].RecordID = "tampered"
	storedAfter := l.ListRecords()
	if storedAfter[0].RecordID == "tampered" {
		t.Fatalf("expected ListRecords to return defensive copy")
	}

	// Audit inclusion proofs for all appended records
	for i := range recs {
		proof, err := l.GetInclusionProof(i)
		if err != nil {
			t.Fatalf("failed to get inclusion proof for index %d: %v", i, err)
		}
		if !ledger.VerifyInclusionProof(proof) {
			t.Fatalf("inclusion proof failed for record %d", i)
		}
		if proof.MerkleRoot != l.CurrentMerkleRoot() {
			t.Fatalf("expected proof MerkleRoot to match current ledger root")
		}
	}

	// Out of bounds proof check
	_, err := l.GetInclusionProof(len(recs))
	if err == nil {
		t.Fatalf("expected error getting inclusion proof beyond record count")
	}
}

func TestScoreBucket(t *testing.T) {
	t.Parallel()

	cases := []struct {
		score    float64
		expected string
	}{
		{5.0, ">=4.5"},
		{4.5, ">=4.5"},
		{4.49, ">=4.0"},
		{4.0, ">=4.0"},
		{3.9, ">=3.5"},
		{3.5, ">=3.5"},
		{3.2, ">=3.0"},
		{3.0, ">=3.0"},
		{2.8, ">=2.5"},
		{2.5, ">=2.5"},
		{2.49, "<2.5"},
		{1.0, "<2.5"},
		{0.0, "<2.5"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(fmt.Sprintf("Score_%.2f", tc.score), func(t *testing.T) {
			t.Parallel()
			got := ledger.ScoreBucket(tc.score)
			if got != tc.expected {
				t.Fatalf("ScoreBucket(%.2f) = %s, expected %s", tc.score, got, tc.expected)
			}
		})
	}
}

func TestLedger_IssueAndVerifyTrustPassport_Success(t *testing.T) {
	t.Parallel()

	l := ledger.NewLedger()

	// Append initial interactions to establish a non-empty root
	_, _, err := l.AppendRecord(ledger.InteractionRecord{
		AppointmentID: "apt-01",
		UserRootDID:   "did:key:user01",
		SubAgentDID:   "did:key:sub01",
		VendorDID:     "did:key:vendor01",
		Status:        "settled",
		AmountUSD:     100.0,
	})
	if err != nil {
		t.Fatalf("failed to append record: %v", err)
	}

	subjectDID := "did:key:rotating-sub-agent-01"
	actualFulfillment := 0.98
	actualBookings := 45
	actualTier := 3
	actualScore := 4.8

	reqMinFulfillment := 0.90
	reqMinBookings := 20
	reqMinTier := 2

	passport, err := l.IssueTrustPassport(
		subjectDID,
		actualFulfillment,
		actualBookings,
		actualTier,
		actualScore,
		reqMinFulfillment,
		reqMinBookings,
		reqMinTier,
	)
	if err != nil {
		t.Fatalf("failed to issue trust passport: %v", err)
	}

	if passport.PassportID == "" {
		t.Fatalf("expected non-empty PassportID")
	}
	if passport.SubjectDID != subjectDID {
		t.Fatalf("expected SubjectDID %s, got %s", subjectDID, passport.SubjectDID)
	}
	if passport.MerkleRoot != l.CurrentMerkleRoot() {
		t.Fatalf("expected MerkleRoot %s, got %s", l.CurrentMerkleRoot(), passport.MerkleRoot)
	}
	if passport.MinFulfillmentRate != reqMinFulfillment {
		t.Fatalf("expected MinFulfillmentRate %.2f, got %.2f", reqMinFulfillment, passport.MinFulfillmentRate)
	}
	if passport.MinSettledBookings != reqMinBookings {
		t.Fatalf("expected MinSettledBookings %d, got %d", reqMinBookings, passport.MinSettledBookings)
	}
	if passport.MinTrustTier != reqMinTier {
		t.Fatalf("expected MinTrustTier %d, got %d", reqMinTier, passport.MinTrustTier)
	}
	if passport.CompositeScoreBucket != ">=4.5" {
		t.Fatalf("expected CompositeScoreBucket >=4.5, got %s", passport.CompositeScoreBucket)
	}
	if len(passport.CommitmentSaltHash) != 64 {
		t.Fatalf("expected 64-char CommitmentSaltHash, got %s", passport.CommitmentSaltHash)
	}
	if len(passport.EnclaveAttestationSig) == 0 {
		t.Fatalf("expected non-empty EnclaveAttestationSig")
	}
	if passport.IssuedAt.IsZero() || passport.ExpiresAt.IsZero() {
		t.Fatalf("expected valid non-zero timestamps")
	}
	if !passport.ExpiresAt.After(passport.IssuedAt) {
		t.Fatalf("expected ExpiresAt to be after IssuedAt")
	}

	// Verification by issuing ledger
	if !l.VerifyTrustPassport(passport) {
		t.Fatalf("expected valid trust passport to pass verification")
	}
}

func TestLedger_IssueTrustPassport_PredicateRejections(t *testing.T) {
	t.Parallel()

	l := ledger.NewLedger()
	subjectDID := "did:key:agent-predicate-fail"

	// 1. Empty SubjectDID
	_, err := l.IssueTrustPassport("", 0.95, 30, 3, 4.5, 0.90, 20, 2)
	if err == nil {
		t.Fatalf("expected error for empty SubjectDID")
	}

	// 2. Fails fulfillment rate
	_, err = l.IssueTrustPassport(subjectDID, 0.85, 30, 3, 4.5, 0.90, 20, 2)
	if err == nil {
		t.Fatalf("expected error for insufficient fulfillment rate")
	}

	// 3. Fails settled bookings count
	_, err = l.IssueTrustPassport(subjectDID, 0.95, 10, 3, 4.5, 0.90, 20, 2)
	if err == nil {
		t.Fatalf("expected error for insufficient settled bookings")
	}

	// 4. Fails trust tier
	_, err = l.IssueTrustPassport(subjectDID, 0.95, 30, 1, 4.5, 0.90, 20, 2)
	if err == nil {
		t.Fatalf("expected error for insufficient trust tier")
	}
}

func TestLedger_VerifyTrustPassport_TamperAndExpiry(t *testing.T) {
	t.Parallel()

	l := ledger.NewLedger()
	p, err := l.IssueTrustPassport("did:key:target", 0.95, 30, 3, 4.2, 0.90, 20, 2)
	if err != nil {
		t.Fatalf("failed to issue passport: %v", err)
	}

	// Baseline
	if !l.VerifyTrustPassport(p) {
		t.Fatalf("baseline passport should be valid")
	}

	// Nil passport
	if l.VerifyTrustPassport(nil) {
		t.Fatalf("nil passport must be rejected")
	}

	// Expired passport
	expired := *p
	expired.ExpiresAt = time.Now().UTC().Add(-1 * time.Hour)
	if l.VerifyTrustPassport(&expired) {
		t.Fatalf("expired passport must be rejected")
	}

	// Tampered SubjectDID
	tamperedDID := *p
	tamperedDID.SubjectDID = "did:key:imposter"
	if l.VerifyTrustPassport(&tamperedDID) {
		t.Fatalf("tampered SubjectDID must be rejected")
	}

	// Tampered MerkleRoot
	tamperedRoot := *p
	tamperedRoot.MerkleRoot = ledger.HashLeaf("fake-root")
	if l.VerifyTrustPassport(&tamperedRoot) {
		t.Fatalf("tampered MerkleRoot must be rejected")
	}

	// Tampered MinFulfillmentRate
	tamperedFulfillment := *p
	tamperedFulfillment.MinFulfillmentRate = 0.99
	if l.VerifyTrustPassport(&tamperedFulfillment) {
		t.Fatalf("tampered MinFulfillmentRate must be rejected")
	}

	// Tampered MinSettledBookings
	tamperedBookings := *p
	tamperedBookings.MinSettledBookings = 100
	if l.VerifyTrustPassport(&tamperedBookings) {
		t.Fatalf("tampered MinSettledBookings must be rejected")
	}

	// Tampered MinTrustTier
	tamperedTier := *p
	tamperedTier.MinTrustTier = 4
	if l.VerifyTrustPassport(&tamperedTier) {
		t.Fatalf("tampered MinTrustTier must be rejected")
	}

	// Tampered ScoreBucket
	tamperedBucket := *p
	tamperedBucket.CompositeScoreBucket = ">=4.5"
	if l.VerifyTrustPassport(&tamperedBucket) {
		t.Fatalf("tampered CompositeScoreBucket must be rejected")
	}

	// Tampered CommitmentSaltHash
	tamperedSalt := *p
	tamperedSalt.CommitmentSaltHash = ledger.HashLeaf("tampered-salt")
	if l.VerifyTrustPassport(&tamperedSalt) {
		t.Fatalf("tampered CommitmentSaltHash must be rejected")
	}

	// Tampered Signature hex
	tamperedSigHex := *p
	tamperedSigHex.EnclaveAttestationSig = "invalid-hex"
	if l.VerifyTrustPassport(&tamperedSigHex) {
		t.Fatalf("invalid hex signature must be rejected")
	}

	// Tampered Signature bytes
	tamperedSigBytes := *p
	sigRaw, _ := hex.DecodeString(p.EnclaveAttestationSig)
	sigRaw[0] ^= 0xFF
	tamperedSigBytes.EnclaveAttestationSig = hex.EncodeToString(sigRaw)
	if l.VerifyTrustPassport(&tamperedSigBytes) {
		t.Fatalf("cryptographically altered signature must be rejected")
	}

	// Different ledger (unrecognized enclave key)
	otherLedger := ledger.NewLedger()
	if otherLedger.VerifyTrustPassport(p) {
		t.Fatalf("foreign ledger enclave must reject passport signed by another enclave")
	}
}

func TestLedger_EnclavePublicKeyCopy(t *testing.T) {
	t.Parallel()

	l := ledger.NewLedger()
	pub := l.EnclavePublicKey()
	if len(pub) != ed25519.PublicKeySize {
		t.Fatalf("expected public key size %d, got %d", ed25519.PublicKeySize, len(pub))
	}

	// Mutating the returned copy must not corrupt internal key
	pub[0] ^= 0xFF
	pub2 := l.EnclavePublicKey()
	if pub[0] == pub2[0] {
		t.Fatalf("expected EnclavePublicKey to return a defensive copy")
	}
}

func TestLedger_ConcurrentAccess_Race(t *testing.T) {
	t.Parallel()

	l := ledger.NewLedger()
	numGoroutines := 16
	opsPerGoroutine := 25

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for g := 0; g < numGoroutines; g++ {
		g := g
		go func() {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				rec := ledger.InteractionRecord{
					AppointmentID: fmt.Sprintf("apt-%d-%d", g, i),
					UserRootDID:   fmt.Sprintf("did:key:user-%d", g),
					SubAgentDID:   fmt.Sprintf("did:key:sub-%d", g),
					VendorDID:     fmt.Sprintf("did:key:vendor-%d", g),
					Status:        "settled",
					AmountUSD:     float64((g+1)*10 + i),
				}

				// Concurrent append
				_, root, err := l.AppendRecord(rec)
				if err != nil {
					t.Errorf("concurrent append failed: %v", err)
					return
				}
				if root == "" {
					t.Errorf("empty root returned from append")
					return
				}

				// Concurrent root query
				_ = l.CurrentMerkleRoot()

				// Concurrent listing
				records := l.ListRecords()
				if len(records) > 0 {
					// Concurrent proof generation on existing record
					target := (g * i) % len(records)
					proof, proofErr := l.GetInclusionProof(target)
					if proofErr == nil && proof != nil {
						_ = ledger.VerifyInclusionProof(proof)
					}
				}

				// Concurrent passport issuance
				passport, passErr := l.IssueTrustPassport(
					fmt.Sprintf("did:key:agent-%d", g),
					0.95,
					50,
					3,
					4.2,
					0.90,
					20,
					2,
				)
				if passErr != nil {
					t.Errorf("concurrent passport issue failed: %v", passErr)
					return
				}

				// Concurrent passport verification
				if !l.VerifyTrustPassport(passport) {
					t.Errorf("concurrent passport verification failed")
					return
				}
			}
		}()
	}

	wg.Wait()

	records := l.ListRecords()
	expectedRecords := numGoroutines * opsPerGoroutine
	if len(records) != expectedRecords {
		t.Fatalf("expected %d records after concurrent writes, got %d", expectedRecords, len(records))
	}
}
