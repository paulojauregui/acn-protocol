package governance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestDefaultConstitution(t *testing.T) {
	c := DefaultConstitution()
	if c.NetworkIntegrityFeeRate != 0.005 {
		t.Fatalf("expected fee rate 0.005, got %f", c.NetworkIntegrityFeeRate)
	}
	if c.VendorSlashShare != 0.70 {
		t.Fatalf("expected vendor slash share 0.70, got %f", c.VendorSlashShare)
	}
	if c.NetworkSlashShare != 0.30 {
		t.Fatalf("expected network slash share 0.30, got %f", c.NetworkSlashShare)
	}
	if c.ProbationaryScoreThreshold != 2.50 {
		t.Fatalf("expected probationary threshold 2.50, got %f", c.ProbationaryScoreThreshold)
	}
	if c.NonceTTLSeconds != 60 {
		t.Fatalf("expected nonce ttl 60, got %d", c.NonceTTLSeconds)
	}

	eng := NewEngine()
	if got := eng.GetConstitution(); got != c {
		t.Fatalf("engine constitution mismatch: got %+v, want %+v", got, c)
	}
}

func TestComputeVoteWeight(t *testing.T) {
	tests := []struct {
		name       string
		stakeUSD   float64
		repScore   float64
		wantWeight float64
	}{
		{
			name:       "zero stake produces zero weight",
			stakeUSD:   0.0,
			repScore:   4.5,
			wantWeight: 0.0,
		},
		{
			name:       "negative stake produces zero weight",
			stakeUSD:   -100.0,
			repScore:   4.5,
			wantWeight: 0.0,
		},
		{
			name:       "zero reputation produces zero weight",
			stakeUSD:   1000.0,
			repScore:   0.0,
			wantWeight: 0.0,
		},
		{
			name:       "negative reputation produces zero weight",
			stakeUSD:   1000.0,
			repScore:   -2.0,
			wantWeight: 0.0,
		},
		{
			name:       "positive stake and normal reputation",
			stakeUSD:   99.0, // ln(1 + 99) = ln(100)
			repScore:   4.0,
			wantWeight: math.Log1p(99.0) * 4.0,
		},
		{
			name:       "reputation clamped to 5.0",
			stakeUSD:   100.0,
			repScore:   9.99,
			wantWeight: math.Log1p(100.0) * 5.0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ComputeVoteWeight(tc.stakeUSD, tc.repScore)
			diff := math.Abs(got - tc.wantWeight)
			if diff > 1e-9 {
				t.Fatalf("ComputeVoteWeight(%f, %f) = %f; want %f", tc.stakeUSD, tc.repScore, got, tc.wantWeight)
			}
		})
	}
}

func TestProposalCreationAndListing(t *testing.T) {
	eng := NewEngine()

	// Empty ID validation
	if _, err := eng.CreateProposal("", "Title", "Desc", "param", "val"); !errors.Is(err, ErrEmptyProposalID) {
		t.Fatalf("expected ErrEmptyProposalID, got %v", err)
	}

	// Empty Title validation
	if _, err := eng.CreateProposal("p1", "", "Desc", "param", "val"); !errors.Is(err, ErrEmptyProposalTitle) {
		t.Fatalf("expected ErrEmptyProposalTitle, got %v", err)
	}

	// Successful creation
	p1, err := eng.CreateProposal("prop-02", "Second", "Description", "fee_rate", "0.01")
	if err != nil {
		t.Fatalf("failed to create proposal: %v", err)
	}
	if p1.ProposalID != "prop-02" || p1.Status != ProposalActive {
		t.Fatalf("unexpected proposal state: %+v", p1)
	}

	// Duplicate creation
	if _, err := eng.CreateProposal("prop-02", "Duplicate", "Desc", "k", "v"); !errors.Is(err, ErrProposalAlreadyExists) {
		t.Fatalf("expected ErrProposalAlreadyExists, got %v", err)
	}

	// Add another proposal to test sorting
	_, err = eng.CreateProposal("prop-01", "First", "Description", "nonce_ttl", "120")
	if err != nil {
		t.Fatalf("failed to create proposal: %v", err)
	}

	list := eng.ListProposals()
	if len(list) != 2 {
		t.Fatalf("expected 2 proposals, got %d", len(list))
	}
	if list[0].ProposalID != "prop-01" || list[1].ProposalID != "prop-02" {
		t.Fatalf("proposals not sorted by ID: %s, %s", list[0].ProposalID, list[1].ProposalID)
	}
}

func TestCastVoteAndStatusTransitions(t *testing.T) {
	eng := NewEngine()
	propID := "prop-voting-test"
	_, err := eng.CreateProposal(propID, "Fee Update", "Adjust fee", "fee_rate", "0.008")
	if err != nil {
		t.Fatalf("failed to create proposal: %v", err)
	}

	// Validation checks
	if _, _, err := eng.CastVote("", "did:acn:voter1", 100, 3.0, true, false); !errors.Is(err, ErrEmptyProposalID) {
		t.Fatalf("expected ErrEmptyProposalID, got %v", err)
	}
	if _, _, err := eng.CastVote(propID, "", 100, 3.0, true, false); !errors.Is(err, ErrEmptyVoterDID) {
		t.Fatalf("expected ErrEmptyVoterDID, got %v", err)
	}
	if _, _, err := eng.CastVote("unknown-prop", "did:acn:voter1", 100, 3.0, true, false); !errors.Is(err, ErrProposalNotFound) {
		t.Fatalf("expected ErrProposalNotFound, got %v", err)
	}

	// First vote: YES
	rcpt1, updatedProp1, err := eng.CastVote(propID, "did:acn:voter1", 1000.0, 4.0, true, false)
	if err != nil {
		t.Fatalf("first vote failed: %v", err)
	}
	expectedWeight1 := ComputeVoteWeight(1000.0, 4.0)
	if math.Abs(rcpt1.VoteWeight-expectedWeight1) > 1e-9 {
		t.Fatalf("expected weight %f, got %f", expectedWeight1, rcpt1.VoteWeight)
	}
	if updatedProp1.Status != ProposalPassed {
		t.Fatalf("expected proposal status PASSED, got %s", updatedProp1.Status)
	}
	if updatedProp1.YesWeight != expectedWeight1 || updatedProp1.NoWeight != 0.0 {
		t.Fatalf("unexpected weights: yes=%f, no=%f", updatedProp1.YesWeight, updatedProp1.NoWeight)
	}

	// Duplicate vote from voter1 rejected
	if _, _, err := eng.CastVote(propID, "did:acn:voter1", 1000.0, 4.0, true, false); !errors.Is(err, ErrAlreadyVoted) {
		t.Fatalf("expected ErrAlreadyVoted, got %v", err)
	}

	// Second vote: NO with greater weight
	rcpt2, updatedProp2, err := eng.CastVote(propID, "did:acn:voter2", 50000.0, 4.5, false, false)
	if err != nil {
		t.Fatalf("second vote failed: %v", err)
	}
	expectedWeight2 := ComputeVoteWeight(50000.0, 4.5)
	if math.Abs(rcpt2.VoteWeight-expectedWeight2) > 1e-9 {
		t.Fatalf("expected weight %f, got %f", expectedWeight2, rcpt2.VoteWeight)
	}
	if updatedProp2.Status != ProposalRejected {
		t.Fatalf("expected proposal status REJECTED, got %s", updatedProp2.Status)
	}
	if updatedProp2.NoWeight != expectedWeight2 {
		t.Fatalf("expected NoWeight %f, got %f", expectedWeight2, updatedProp2.NoWeight)
	}
}

func TestValidatorVetoOverridingMajority(t *testing.T) {
	eng := NewEngine()
	propID := "prop-veto-test"
	_, err := eng.CreateProposal(propID, "Emergency Change", "Protocol upgrade", "upgrade_tag", "v2.0.0")
	if err != nil {
		t.Fatalf("failed to create proposal: %v", err)
	}

	// Whales vote overwhelming YES
	_, p1, err := eng.CastVote(propID, "did:acn:whale1", 1000000.0, 5.0, true, false)
	if err != nil {
		t.Fatalf("whale vote failed: %v", err)
	}
	if p1.Status != ProposalPassed {
		t.Fatalf("expected PASSED before veto, got %s", p1.Status)
	}

	// Validator casts absolute veto
	rcptVeto, pVeto, err := eng.CastVote(propID, "did:acn:validator1", 10000.0, 4.8, false, true)
	if err != nil {
		t.Fatalf("validator veto failed: %v", err)
	}
	if !rcptVeto.ValidatorVeto {
		t.Fatalf("expected receipt ValidatorVeto to be true")
	}
	if !pVeto.ValidatorVetoed {
		t.Fatalf("expected proposal ValidatorVetoed to be true")
	}
	if pVeto.Status != ProposalVetoed {
		t.Fatalf("expected status VETOED_BY_VALIDATORS, got %s", pVeto.Status)
	}

	// Additional YES votes cast afterward cannot overturn validator veto
	_, pAfter, err := eng.CastVote(propID, "did:acn:whale2", 5000000.0, 5.0, true, false)
	if err != nil {
		t.Fatalf("subsequent vote failed: %v", err)
	}
	if pAfter.Status != ProposalVetoed {
		t.Fatalf("expected proposal to remain VETOED_BY_VALIDATORS, got %s", pAfter.Status)
	}
}

func TestArbitrateDisputeDeterministic(t *testing.T) {
	jury := NewJury("", false)
	ctx := context.Background()

	// Validation checks
	if _, err := jury.ArbitrateDispute(ctx, DisputeCase{CaseID: "", AppointmentID: "apt-1"}); !errors.Is(err, ErrEmptyCaseID) {
		t.Fatalf("expected ErrEmptyCaseID, got %v", err)
	}
	if _, err := jury.ArbitrateDispute(ctx, DisputeCase{CaseID: "case-1", AppointmentID: ""}); !errors.Is(err, ErrEmptyAppointmentID) {
		t.Fatalf("expected ErrEmptyAppointmentID, got %v", err)
	}

	// Case 1: POS Receipt + Local Beacon -> User wins, Vendor slashed
	c1 := DisputeCase{
		CaseID:              "case-pos-beacon",
		AppointmentID:       "apt-101",
		UserDID:             "did:acn:user1",
		VendorDID:           "did:acn:vendor1",
		StripePOSReceipt:    true,
		LocalBeaconVerified: true,
	}
	v1, err := jury.ArbitrateDispute(ctx, c1)
	if err != nil {
		t.Fatalf("arbitration failed: %v", err)
	}
	if v1.Winner != WinnerUser || v1.SlashUser != false || v1.SlashVendor != true {
		t.Fatalf("unexpected verdict for POS+Beacon: %+v", v1)
	}
	if v1.EngineUsed != EngineDeterministic {
		t.Fatalf("expected EngineDeterministic, got %s", v1.EngineUsed)
	}

	// Retrieve stored verdict
	retrievedV1, ok := jury.GetVerdict(c1.CaseID)
	if !ok || retrievedV1.CaseID != c1.CaseID {
		t.Fatalf("failed to retrieve stored verdict: %+v", retrievedV1)
	}

	// Case 2: One signal (Beacon) + Vendor conflicts >= 2 -> User wins, Vendor slashed
	c2 := DisputeCase{
		CaseID:              "case-conflict-vendor",
		AppointmentID:       "apt-102",
		UserDID:             "did:acn:user2",
		VendorDID:           "did:acn:vendor2",
		StripePOSReceipt:    false,
		LocalBeaconVerified: true,
		VendorConflictCount: 3,
	}
	v2, err := jury.ArbitrateDispute(ctx, c2)
	if err != nil {
		t.Fatalf("arbitration failed: %v", err)
	}
	if v2.Winner != WinnerUser || v2.SlashVendor != true || v2.SlashUser != false {
		t.Fatalf("unexpected verdict for conflict vendor: %+v", v2)
	}

	// Case 3: No POS, No Beacon, Vendor absent -> Vendor wins, User slashed
	c3 := DisputeCase{
		CaseID:              "case-user-noshow",
		AppointmentID:       "apt-103",
		UserDID:             "did:acn:user3",
		VendorDID:           "did:acn:vendor3",
		StripePOSReceipt:    false,
		LocalBeaconVerified: false,
		VendorClaimPresent:  false,
	}
	v3, err := jury.ArbitrateDispute(ctx, c3)
	if err != nil {
		t.Fatalf("arbitration failed: %v", err)
	}
	if v3.Winner != WinnerVendor || v3.SlashUser != true || v3.SlashVendor != false {
		t.Fatalf("unexpected verdict for user no-show: %+v", v3)
	}

	// Case 4: No POS, No Beacon, VendorClaimPresent is true -> Vendor wins, User slashed
	c4 := DisputeCase{
		CaseID:              "case-user-unsupported",
		AppointmentID:       "apt-104",
		UserDID:             "did:acn:user4",
		VendorDID:           "did:acn:vendor4",
		StripePOSReceipt:    false,
		LocalBeaconVerified: false,
		VendorClaimPresent:  true,
	}
	v4, err := jury.ArbitrateDispute(ctx, c4)
	if err != nil {
		t.Fatalf("arbitration failed: %v", err)
	}
	if v4.Winner != WinnerVendor || v4.SlashUser != true || v4.SlashVendor != false {
		t.Fatalf("unexpected verdict for unsupported user: %+v", v4)
	}
}

func TestArbitrateDisputeRemoteSparkAudit(t *testing.T) {
	expectedLLMRationale := "AI Audit Jury verified hardware beacon trace and corroborated vendor history."

	// Mock server returning successful OpenAI chat completion
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req openAIChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		resp := openAIChatResponse{
			Choices: []openAIChatChoice{
				{
					Message: openAIChatMessage{
						Role:    "assistant",
						Content: expectedLLMRationale,
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer mockServer.Close()

	jury := NewJury(mockServer.URL, true)
	c := DisputeCase{
		CaseID:              "case-spark-audit",
		AppointmentID:       "apt-201",
		UserDID:             "did:acn:user-ai",
		VendorDID:           "did:acn:vendor-ai",
		StripePOSReceipt:    true,
		LocalBeaconVerified: true,
	}

	verdict, err := jury.ArbitrateDispute(context.Background(), c)
	if err != nil {
		t.Fatalf("remote arbitration failed: %v", err)
	}
	if verdict.EngineUsed != EngineDGXSparkLLM {
		t.Fatalf("expected EngineDGXSparkLLM, got %s", verdict.EngineUsed)
	}
	if verdict.Rationale != expectedLLMRationale {
		t.Fatalf("expected rationale %q, got %q", expectedLLMRationale, verdict.Rationale)
	}
}

func TestArbitrateDisputeSparkFailover(t *testing.T) {
	// Scenario 1: HTTP 500 server error
	errorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal vLLM inference engine fault", http.StatusInternalServerError)
	}))
	defer errorServer.Close()

	jury500 := NewJury(errorServer.URL, true)
	c1 := DisputeCase{
		CaseID:              "case-failover-500",
		AppointmentID:       "apt-301",
		UserDID:             "did:acn:user1",
		VendorDID:           "did:acn:vendor1",
		StripePOSReceipt:    true,
		LocalBeaconVerified: true,
	}

	v1, err := jury500.ArbitrateDispute(context.Background(), c1)
	if err != nil {
		t.Fatalf("expected seamless fallback without error, got: %v", err)
	}
	if v1.EngineUsed != EngineDeterministic {
		t.Fatalf("expected fallback to EngineDeterministic, got %s", v1.EngineUsed)
	}
	if v1.Winner != WinnerUser || !v1.SlashVendor {
		t.Fatalf("fallback verdict incorrect: %+v", v1)
	}

	// Scenario 2: Slow/Timeout server fallback
	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"late"}}]}`))
	}))
	defer slowServer.Close()

	// Short context to force timeout
	juryTimeout := NewJury(slowServer.URL, true)
	shortCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	c2 := DisputeCase{
		CaseID:              "case-failover-timeout",
		AppointmentID:       "apt-302",
		UserDID:             "did:acn:user2",
		VendorDID:           "did:acn:vendor2",
		StripePOSReceipt:    false,
		LocalBeaconVerified: false,
		VendorClaimPresent:  false,
	}

	v2, err := juryTimeout.ArbitrateDispute(shortCtx, c2)
	if err != nil {
		t.Fatalf("expected seamless fallback on timeout, got: %v", err)
	}
	if v2.EngineUsed != EngineDeterministic {
		t.Fatalf("expected fallback to EngineDeterministic on timeout, got %s", v2.EngineUsed)
	}
	if v2.Winner != WinnerVendor || !v2.SlashUser {
		t.Fatalf("timeout fallback verdict incorrect: %+v", v2)
	}
}

func TestConcurrentGovernanceOperations(t *testing.T) {
	eng := NewEngine()
	jury := NewJury("", false)

	const numProposals = 10
	for i := 0; i < numProposals; i++ {
		id := fmt.Sprintf("concurrent-prop-%d", i)
		if _, err := eng.CreateProposal(id, "Proposal", "Desc", "key", "val"); err != nil {
			t.Fatalf("failed to create initial proposal: %v", err)
		}
	}

	var wg sync.WaitGroup
	const workers = 20

	// Concurrent voters
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			propID := fmt.Sprintf("concurrent-prop-%d", workerID%numProposals)
			voterDID := fmt.Sprintf("did:acn:voter-%d", workerID)
			support := (workerID % 2) == 0
			veto := workerID == 19 // Last worker attempts veto

			_, _, _ = eng.CastVote(propID, voterDID, float64(100*(workerID+1)), 3.5, support, veto)
			_ = eng.GetConstitution()
			_ = eng.ListProposals()
		}(i)
	}

	// Concurrent jury arbitrations
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			caseID := fmt.Sprintf("concurrent-case-%d", workerID)
			aptID := fmt.Sprintf("concurrent-apt-%d", workerID)
			c := DisputeCase{
				CaseID:              caseID,
				AppointmentID:       aptID,
				UserDID:             fmt.Sprintf("did:acn:user-%d", workerID),
				VendorDID:           fmt.Sprintf("did:acn:vendor-%d", workerID),
				StripePOSReceipt:    workerID%2 == 0,
				LocalBeaconVerified: workerID%3 == 0,
				VendorConflictCount: workerID % 4,
			}
			_, _ = jury.ArbitrateDispute(context.Background(), c)
			_, _ = jury.GetVerdict(caseID)
		}(i)
	}

	wg.Wait()
}
