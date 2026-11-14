package api_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/paulojauregui/acn-protocol/internal/api"
	"github.com/paulojauregui/acn-protocol/internal/identity"
	"github.com/paulojauregui/acn-protocol/internal/ledger"
	"github.com/paulojauregui/acn-protocol/internal/oracle"
	"github.com/paulojauregui/acn-protocol/internal/relay"
	"github.com/paulojauregui/acn-protocol/internal/simulator"
)

// TestServer_HealthzAndReady verifies liveness and readiness probe endpoints.
func TestServer_HealthzAndReady(t *testing.T) {
	s := api.NewServer()
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	// 1. Healthz probe
	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	var health api.HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Fatalf("failed to decode health response: %v", err)
	}
	if health.Status != "healthy" || health.Service != "acn-protocol" || health.Version != "1.0.0" {
		t.Fatalf("unexpected health payload: %+v", health)
	}

	// 2. Ready probe
	readyResp, err := http.Get(ts.URL + "/ready")
	if err != nil {
		t.Fatalf("GET /ready failed: %v", err)
	}
	defer readyResp.Body.Close()

	if readyResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", readyResp.StatusCode)
	}

	var ready api.ReadyResponse
	if err := json.NewDecoder(readyResp.Body).Decode(&ready); err != nil {
		t.Fatalf("failed to decode ready response: %v", err)
	}
	if ready.Status != "ready" || ready.MerkleRoot == "" || ready.RegisteredAgents < 7 {
		t.Fatalf("unexpected ready payload: %+v", ready)
	}
}

// TestServer_EmbeddedUI verifies serving the embedded HTML dashboard console.
func TestServer_EmbeddedUI(t *testing.T) {
	s := api.NewServer()
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET / failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/html") {
		t.Fatalf("expected Content-Type text/html, got %s", contentType)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}
	if !strings.Contains(string(body), "ACN Protocol") {
		t.Fatalf("expected page body to contain 'ACN Protocol', got: %s", string(body))
	}
}

// TestServer_Overview verifies aggregate network telemetry.
func TestServer_Overview(t *testing.T) {
	s := api.NewServer()
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/overview")
	if err != nil {
		t.Fatalf("GET /api/v1/overview failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	var overview api.OverviewResponse
	if err := json.NewDecoder(resp.Body).Decode(&overview); err != nil {
		t.Fatalf("failed to decode overview response: %v", err)
	}

	if overview.MerkleRoot == "" {
		t.Fatal("expected non-empty merkle root")
	}
	if overview.RegisteredAgents != 7 {
		t.Fatalf("expected 7 registered root agents, got %d", overview.RegisteredAgents)
	}
	if overview.ActiveSubAgents != 3 {
		t.Fatalf("expected 3 pre-seeded sub-agents, got %d", overview.ActiveSubAgents)
	}
	if overview.LedgerRecordCount < 4 {
		t.Fatalf("expected at least 4 ledger records, got %d", overview.LedgerRecordCount)
	}
	if overview.ProposalCount < 2 {
		t.Fatalf("expected at least 2 governance proposals, got %d", overview.ProposalCount)
	}
	if overview.Escrow.TotalAvailableUSD <= 0 {
		t.Fatalf("expected positive total available escrow bond, got %.2f", overview.Escrow.TotalAvailableUSD)
	}
}

// TestServer_GetAndPostAgents verifies agent enumeration, registration, sub-agent minting, and bond deposits.
func TestServer_GetAndPostAgents(t *testing.T) {
	s := api.NewServer()
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	// 1. GET /api/v1/agents - verify pre-seeded agents
	resp, err := http.Get(ts.URL + "/api/v1/agents")
	if err != nil {
		t.Fatalf("GET /api/v1/agents failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	var agentsResp api.AgentsResponse
	if err := json.NewDecoder(resp.Body).Decode(&agentsResp); err != nil {
		t.Fatalf("failed to decode agents response: %v", err)
	}

	if len(agentsResp.RootAgents) != 7 {
		t.Fatalf("expected 7 root agents, got %d", len(agentsResp.RootAgents))
	}
	if len(agentsResp.SubAgents) != 3 {
		t.Fatalf("expected 3 sub agents, got %d", len(agentsResp.SubAgents))
	}

	// Verify Alice (Tier 3), Bob (Tier 2), Charlie (Tier 1), Eve (Tier 0)
	var aliceFound, bobFound, charlieFound, eveFound bool
	var aliceDID string
	for _, item := range agentsResp.RootAgents {
		switch item.Agent.DisplayName {
		case "Alice Sovereign Executive":
			aliceFound = true
			aliceDID = item.Agent.RootDID
			if item.Agent.TrustTier != identity.Tier3Sovereign {
				t.Fatalf("expected Alice to be Tier3Sovereign, got %s", item.Agent.TrustTier)
			}
			if item.Agent.BondUSD != 500 || item.Agent.CumulativeSpendUSD != 2400 || item.Agent.SettledTxCount != 35 {
				t.Fatalf("unexpected Alice metrics: %+v", item.Agent)
			}
		case "Bob Reputational Traveler":
			bobFound = true
			if item.Agent.TrustTier != identity.Tier2Reputational {
				t.Fatalf("expected Bob to be Tier2Reputational, got %s", item.Agent.TrustTier)
			}
			if item.Agent.BondUSD != 150 || item.Agent.CumulativeSpendUSD != 750 || item.Agent.SettledTxCount != 22 {
				t.Fatalf("unexpected Bob metrics: %+v", item.Agent)
			}
		case "Charlie Economic Commuter":
			charlieFound = true
			if item.Agent.TrustTier != identity.Tier1Economic {
				t.Fatalf("expected Charlie to be Tier1Economic, got %s", item.Agent.TrustTier)
			}
			if item.Agent.BondUSD != 60 || item.Agent.CumulativeSpendUSD != 120 || item.Agent.SettledTxCount != 6 {
				t.Fatalf("unexpected Charlie metrics: %+v", item.Agent)
			}
		case "Eve Unbonded Ephemeral Bot":
			eveFound = true
			if item.Agent.TrustTier != identity.Tier0Ephemeral {
				t.Fatalf("expected Eve to be Tier0Ephemeral, got %s", item.Agent.TrustTier)
			}
		}
	}

	if !aliceFound || !bobFound || !charlieFound || !eveFound {
		t.Fatalf("missing expected user agents: alice=%t bob=%t charlie=%t eve=%t", aliceFound, bobFound, charlieFound, eveFound)
	}

	// 2. POST /api/v1/agents - register a new Root DID
	regPayload := api.AgentActionRequest{
		Action:         "register_root",
		DisplayName:    "Dave Enterprise Delegate",
		PoPVerified:    true,
		TEEAttested:    true,
		IsVendor:       false,
		DepositBondUSD: 300.0,
	}
	bodyBytes, _ := json.Marshal(regPayload)
	regResp, err := http.Post(ts.URL+"/api/v1/agents", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("POST /api/v1/agents register_root failed: %v", err)
	}
	defer regResp.Body.Close()

	if regResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d", regResp.StatusCode)
	}

	var regResult map[string]any
	if err := json.NewDecoder(regResp.Body).Decode(&regResult); err != nil {
		t.Fatalf("failed to decode register_root result: %v", err)
	}
	daveAgentMap, ok := regResult["agent"].(map[string]any)
	if !ok {
		t.Fatalf("expected agent in register_root response, got: %+v", regResult)
	}
	daveDID := daveAgentMap["root_did"].(string)

	// 3. POST /api/v1/agents - spawn a rotating Sub-Agent for Alice
	subPayload := api.AgentActionRequest{
		Action:     "spawn_sub",
		RootDID:    aliceDID,
		Purpose:    "ephemeral-ride-booking",
		TTLSeconds: 3600,
	}
	subBody, _ := json.Marshal(subPayload)
	subResp, err := http.Post(ts.URL+"/api/v1/agents", "application/json", bytes.NewReader(subBody))
	if err != nil {
		t.Fatalf("POST /api/v1/agents spawn_sub failed: %v", err)
	}
	defer subResp.Body.Close()

	if subResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created for spawn_sub, got %d", subResp.StatusCode)
	}

	var subResult map[string]any
	if err := json.NewDecoder(subResp.Body).Decode(&subResult); err != nil {
		t.Fatalf("failed to decode spawn_sub result: %v", err)
	}
	subAgentMap := subResult["sub_agent"].(map[string]any)
	subDID := subAgentMap["sub_did"].(string)
	if !strings.HasPrefix(subDID, identity.SubDIDPrefix) {
		t.Fatalf("expected sub DID prefix %s, got %s", identity.SubDIDPrefix, subDID)
	}

	// 4. POST /api/v1/agents - deposit additional bond
	depPayload := api.AgentActionRequest{
		Action:    "deposit_bond",
		RootDID:   daveDID,
		AmountUSD: 200.0,
	}
	depBody, _ := json.Marshal(depPayload)
	depResp, err := http.Post(ts.URL+"/api/v1/agents", "application/json", bytes.NewReader(depBody))
	if err != nil {
		t.Fatalf("POST /api/v1/agents deposit_bond failed: %v", err)
	}
	defer depResp.Body.Close()

	if depResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for deposit_bond, got %d", depResp.StatusCode)
	}

	var depResult map[string]any
	if err := json.NewDecoder(depResp.Body).Decode(&depResult); err != nil {
		t.Fatalf("failed to decode deposit_bond result: %v", err)
	}
	accMap := depResult["account"].(map[string]any)
	if accMap["available_bond_usd"].(float64) < 500.0 {
		t.Fatalf("expected total available bond >= 500.0, got %v", accMap["available_bond_usd"])
	}
}

// TestServer_RelayBooking verifies SIP-style Agent-to-Agent booking negotiation.
func TestServer_RelayBooking(t *testing.T) {
	s := api.NewServer()
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	// Find Alice and Waymo DIDs
	roots := s.Registry.ListRootAgents()
	var aliceDID, waymoDID, eveDID string
	for _, r := range roots {
		if r.DisplayName == "Alice Sovereign Executive" {
			aliceDID = r.RootDID
		}
		if r.DisplayName == "Autonomous Waymo Fleet SF" {
			waymoDID = r.RootDID
		}
		if r.DisplayName == "Eve Unbonded Ephemeral Bot" {
			eveDID = r.RootDID
		}
	}

	// 1. Successful booking: Alice (Tier 3, 500 bond) booking Waymo for $40
	bookReq := api.RelayBookRequest{
		CallerDID:         aliceDID,
		VendorDID:         waymoDID,
		ServiceName:       "autonomous-cab-tier3",
		BondAmountUSD:     40.0,
		MinRequiredScore:  3.5,
		RequiredTrustTier: 2,
	}
	body, _ := json.Marshal(bookReq)
	resp, err := http.Post(ts.URL+"/api/v1/relay/book", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/v1/relay/book failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	var session relay.NegotiationSession
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		t.Fatalf("failed to decode negotiation session: %v", err)
	}

	if session.State != relay.StateConfirmed {
		t.Fatalf("expected session state CONFIRMED, got %s (rejection: %s)", session.State, session.RejectionReason)
	}
	if session.LockID == "" || session.PassportID == "" {
		t.Fatalf("expected lock ID and passport ID on confirmed session: %+v", session)
	}

	// 2. Rejected booking: Eve (Tier 0, 0 bond) attempts high-value booking requiring Tier 2 and $200 bond
	badReq := api.RelayBookRequest{
		CallerDID:         eveDID,
		VendorDID:         waymoDID,
		ServiceName:       "autonomous-cab-tier3",
		BondAmountUSD:     200.0,
		MinRequiredScore:  3.0,
		RequiredTrustTier: 2,
	}
	badBody, _ := json.Marshal(badReq)
	badResp, err := http.Post(ts.URL+"/api/v1/relay/book", "application/json", bytes.NewReader(badBody))
	if err != nil {
		t.Fatalf("POST /api/v1/relay/book with low trust failed: %v", err)
	}
	defer badResp.Body.Close()

	if badResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK carrying rejected session, got %d", badResp.StatusCode)
	}

	var badSession relay.NegotiationSession
	if err := json.NewDecoder(badResp.Body).Decode(&badSession); err != nil {
		t.Fatalf("failed to decode bad session: %v", err)
	}

	if badSession.State != relay.StateRejectedLowTrust {
		t.Fatalf("expected session state REJECTED_LOW_TRUST, got %s", badSession.State)
	}
	if badSession.RejectionReason == "" {
		t.Fatal("expected non-empty rejection reason")
	}
}

// TestServer_OracleChallengeAndSettlement verifies double-blind check-in, presence challenges, and settlement.
func TestServer_OracleChallengeAndSettlement(t *testing.T) {
	s := api.NewServer()
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	roots := s.Registry.ListRootAgents()
	var aliceDID, waymoDID string
	for _, r := range roots {
		if r.DisplayName == "Alice Sovereign Executive" {
			aliceDID = r.RootDID
		}
		if r.DisplayName == "Autonomous Waymo Fleet SF" {
			waymoDID = r.RootDID
		}
	}

	// 1. Issue challenge nonce
	chalReq := api.OracleChallengeRequest{
		AppointmentID: "apt-test-settle-01",
		UserDID:       aliceDID,
		VendorDID:     waymoDID,
	}
	chalBody, _ := json.Marshal(chalReq)
	cResp, err := http.Post(ts.URL+"/api/v1/oracle/challenge", "application/json", bytes.NewReader(chalBody))
	if err != nil {
		t.Fatalf("POST /api/v1/oracle/challenge failed: %v", err)
	}
	defer cResp.Body.Close()

	if cResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d", cResp.StatusCode)
	}

	var nonce oracle.ChallengeNonce
	if err := json.NewDecoder(cResp.Body).Decode(&nonce); err != nil {
		t.Fatalf("failed to decode challenge nonce: %v", err)
	}
	if nonce.Nonce == "" || nonce.AppointmentID != "apt-test-settle-01" {
		t.Fatalf("unexpected challenge nonce: %+v", nonce)
	}

	// 2. Lock bond in escrow vault for this appointment
	lock, err := s.Vault.LockBond("apt-test-settle-01", aliceDID, aliceDID, waymoDID, 50.0)
	if err != nil {
		t.Fatalf("failed to lock bond: %v", err)
	}

	// 3. Settle appointment successfully (mutual presence)
	settleReq := api.OracleSettleRequest{
		AppointmentID:       "apt-test-settle-01",
		LockID:              lock.LockID,
		Nonce:               nonce.Nonce,
		UserRootDID:         aliceDID,
		UserDID:             aliceDID,
		VendorDID:           waymoDID,
		UserPresent:         true,
		VendorPresent:       true,
		StripePOSReceipt:    true,
		LocalBeaconVerified: true,
		PeerRating:          5.0,
		AmountUSD:           50.0,
	}
	sBody, _ := json.Marshal(settleReq)
	sResp, err := http.Post(ts.URL+"/api/v1/oracle/settle", "application/json", bytes.NewReader(sBody))
	if err != nil {
		t.Fatalf("POST /api/v1/oracle/settle failed: %v", err)
	}
	defer sResp.Body.Close()

	if sResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", sResp.StatusCode)
	}

	var settleResult api.SettleResponse
	if err := json.NewDecoder(sResp.Body).Decode(&settleResult); err != nil {
		t.Fatalf("failed to decode settle response: %v", err)
	}

	if settleResult.Outcome.Status != oracle.StatusSuccess {
		t.Fatalf("expected STATUS_SUCCESS, got %s", settleResult.Outcome.Status)
	}
	if settleResult.Receipt == nil || settleResult.Receipt.ReleasedToUserUSD <= 0 {
		t.Fatalf("expected valid settlement receipt with released funds: %+v", settleResult.Receipt)
	}
	if settleResult.MerkleRoot == "" {
		t.Fatal("expected updated Merkle root in settlement result")
	}

	// 4. Test Slashed scenario (both confirm user absence)
	nonce2, err := s.Oracle.IssueChallengeNonce("apt-test-slash-02", aliceDID, waymoDID)
	if err != nil {
		t.Fatalf("failed to issue second nonce: %v", err)
	}
	lock2, err := s.Vault.LockBond("apt-test-slash-02", aliceDID, aliceDID, waymoDID, 20.0)
	if err != nil {
		t.Fatalf("failed to lock bond 2: %v", err)
	}

	slashReq := api.OracleSettleRequest{
		AppointmentID:       "apt-test-slash-02",
		LockID:              lock2.LockID,
		Nonce:               nonce2.Nonce,
		UserRootDID:         aliceDID,
		UserDID:             aliceDID,
		VendorDID:           waymoDID,
		UserPresent:         false,
		VendorPresent:       false,
		StripePOSReceipt:    false,
		LocalBeaconVerified: false,
		PeerRating:          1.0,
		AmountUSD:           20.0,
	}
	slBody, _ := json.Marshal(slashReq)
	slResp, err := http.Post(ts.URL+"/api/v1/oracle/settle", "application/json", bytes.NewReader(slBody))
	if err != nil {
		t.Fatalf("POST /api/v1/oracle/settle for slash failed: %v", err)
	}
	defer slResp.Body.Close()

	if slResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for slash, got %d", slResp.StatusCode)
	}

	var slashResult api.SettleResponse
	if err := json.NewDecoder(slResp.Body).Decode(&slashResult); err != nil {
		t.Fatalf("failed to decode slash response: %v", err)
	}
	if slashResult.Outcome.Status != oracle.StatusSlashed {
		t.Fatalf("expected STATUS_SLASHED, got %s", slashResult.Outcome.Status)
	}
	if !slashResult.Outcome.UserSlashed {
		t.Fatal("expected UserSlashed to be true")
	}
}

// TestServer_RaaSPassportAndVerify tests selective-disclosure TrustPassport issuance and enclave verification.
func TestServer_RaaSPassportAndVerify(t *testing.T) {
	s := api.NewServer()
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	roots := s.Registry.ListRootAgents()
	var aliceDID, charlieDID string
	for _, r := range roots {
		if r.DisplayName == "Alice Sovereign Executive" {
			aliceDID = r.RootDID
		}
		if r.DisplayName == "Charlie Economic Commuter" {
			charlieDID = r.RootDID
		}
	}

	// 1. Issue passport for Alice: requires fulfillment >= 0.90, bookings >= 10, Tier >= 2
	passReq := api.PassportIssueRequest{
		SubjectDID:        aliceDID,
		ReqMinFulfillment: 0.90,
		ReqMinBookings:    10,
		ReqMinTier:        2,
	}
	pBody, _ := json.Marshal(passReq)
	pResp, err := http.Post(ts.URL+"/api/v1/raas/passport", "application/json", bytes.NewReader(pBody))
	if err != nil {
		t.Fatalf("POST /api/v1/raas/passport failed: %v", err)
	}
	defer pResp.Body.Close()

	if pResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", pResp.StatusCode)
	}

	var passport ledger.TrustPassport
	if err := json.NewDecoder(pResp.Body).Decode(&passport); err != nil {
		t.Fatalf("failed to decode trust passport: %v", err)
	}

	if passport.PassportID == "" || passport.EnclaveAttestationSig == "" {
		t.Fatalf("expected signed passport, got: %+v", passport)
	}
	if passport.SubjectDID != aliceDID {
		t.Fatalf("expected subject DID %s, got %s", aliceDID, passport.SubjectDID)
	}

	// 2. Verify issued passport
	vBody, _ := json.Marshal(passport)
	vResp, err := http.Post(ts.URL+"/api/v1/raas/verify", "application/json", bytes.NewReader(vBody))
	if err != nil {
		t.Fatalf("POST /api/v1/raas/verify failed: %v", err)
	}
	defer vResp.Body.Close()

	if vResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", vResp.StatusCode)
	}

	var vResult api.PassportVerifyResponse
	if err := json.NewDecoder(vResp.Body).Decode(&vResult); err != nil {
		t.Fatalf("failed to decode verify response: %v", err)
	}

	if !vResult.Valid {
		t.Fatal("expected passport to be cryptographically valid")
	}
	if vResult.PassportID != passport.PassportID {
		t.Fatalf("expected passport ID %s, got %s", passport.PassportID, vResult.PassportID)
	}

	// 3. Predicate failure: Charlie is Tier 1, requesting Tier 3 should fail
	failReq := api.PassportIssueRequest{
		SubjectDID:        charlieDID,
		ReqMinFulfillment: 0.50,
		ReqMinBookings:    1,
		ReqMinTier:        3,
	}
	fBody, _ := json.Marshal(failReq)
	fResp, err := http.Post(ts.URL+"/api/v1/raas/passport", "application/json", bytes.NewReader(fBody))
	if err != nil {
		t.Fatalf("POST /api/v1/raas/passport for failure failed: %v", err)
	}
	defer fResp.Body.Close()

	if fResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for failed predicate, got %d", fResp.StatusCode)
	}
}

// TestServer_LedgerMerkle verifies ledger retrieval and inclusion proof generation.
func TestServer_LedgerMerkle(t *testing.T) {
	s := api.NewServer()
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	// 1. Fetch Merkle root and leaves
	resp, err := http.Get(ts.URL + "/api/v1/ledger/merkle")
	if err != nil {
		t.Fatalf("GET /api/v1/ledger/merkle failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	var mResp api.MerkleLedgerResponse
	if err := json.NewDecoder(resp.Body).Decode(&mResp); err != nil {
		t.Fatalf("failed to decode ledger merkle response: %v", err)
	}

	if mResp.MerkleRoot == "" {
		t.Fatal("expected non-empty Merkle root")
	}
	if len(mResp.Records) < 4 {
		t.Fatalf("expected at least 4 records, got %d", len(mResp.Records))
	}

	// 2. Fetch inclusion proof for index 0
	pResp, err := http.Get(ts.URL + "/api/v1/ledger/merkle?index=0")
	if err != nil {
		t.Fatalf("GET /api/v1/ledger/merkle?index=0 failed: %v", err)
	}
	defer pResp.Body.Close()

	if pResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", pResp.StatusCode)
	}

	var pResult api.MerkleLedgerResponse
	if err := json.NewDecoder(pResp.Body).Decode(&pResult); err != nil {
		t.Fatalf("failed to decode proof response: %v", err)
	}

	if pResult.InclusionProof == nil {
		t.Fatal("expected non-nil inclusion proof for index 0")
	}
	if !ledger.VerifyInclusionProof(pResult.InclusionProof) {
		t.Fatal("expected inclusion proof to verify against Merkle root")
	}
}

// TestServer_GovernanceProposalsAndVoting verifies proposal retrieval and reputation-weighted voting.
func TestServer_GovernanceProposalsAndVoting(t *testing.T) {
	s := api.NewServer()
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	roots := s.Registry.ListRootAgents()
	var aliceDID string
	for _, r := range roots {
		if r.DisplayName == "Alice Sovereign Executive" {
			aliceDID = r.RootDID
		}
	}

	// 1. List proposals
	resp, err := http.Get(ts.URL + "/api/v1/governance/proposals")
	if err != nil {
		t.Fatalf("GET /api/v1/governance/proposals failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	var govResp api.GovernanceProposalsResponse
	if err := json.NewDecoder(resp.Body).Decode(&govResp); err != nil {
		t.Fatalf("failed to decode governance response: %v", err)
	}

	if len(govResp.Proposals) < 2 {
		t.Fatalf("expected at least 2 proposals, got %d", len(govResp.Proposals))
	}
	if govResp.Constitution.NetworkIntegrityFeeRate <= 0 {
		t.Fatalf("expected positive fee rate in constitution: %+v", govResp.Constitution)
	}

	targetProposalID := govResp.Proposals[0].ProposalID

	// 2. Cast vote
	voteReq := api.VoteRequest{
		ProposalID: targetProposalID,
		VoterDID:   aliceDID,
		Support:    true,
	}
	vBody, _ := json.Marshal(voteReq)
	vResp, err := http.Post(ts.URL+"/api/v1/governance/vote", "application/json", bytes.NewReader(vBody))
	if err != nil {
		t.Fatalf("POST /api/v1/governance/vote failed: %v", err)
	}
	defer vResp.Body.Close()

	if vResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", vResp.StatusCode)
	}

	var voteResult api.VoteResponse
	if err := json.NewDecoder(vResp.Body).Decode(&voteResult); err != nil {
		t.Fatalf("failed to decode vote response: %v", err)
	}

	if voteResult.Receipt == nil || voteResult.Receipt.VoteWeight <= 0 {
		t.Fatalf("expected positive vote weight in receipt: %+v", voteResult.Receipt)
	}
	if voteResult.Proposal.YesWeight <= 0 {
		t.Fatalf("expected positive YesWeight on proposal: %+v", voteResult.Proposal)
	}
}

// TestServer_Simulations verifies adversarial threat simulator endpoints.
func TestServer_Simulations(t *testing.T) {
	s := api.NewServer()
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	scenarios := []string{"sybil-swarm", "whale-attack", "collusive-vendor"}
	for _, sc := range scenarios {
		resp, err := http.Post(ts.URL+"/api/v1/simulate/"+sc, "application/json", nil)
		if err != nil {
			t.Fatalf("POST /api/v1/simulate/%s failed: %v", sc, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 OK for %s, got %d", sc, resp.StatusCode)
		}

		var result simulator.SimulationResult
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("failed to decode simulation result for %s: %v", sc, err)
		}

		if !result.DefenseSucceeded {
			t.Fatalf("expected DefenseSucceeded true for %s, got false (summary: %s)", sc, result.Summary)
		}
		if result.DurationMs <= 0 {
			t.Fatalf("expected positive DurationMs for %s, got %f", sc, result.DurationMs)
		}
	}

	// Test unknown scenario
	badResp, err := http.Post(ts.URL+"/api/v1/simulate/unknown-exploit", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /api/v1/simulate/unknown-exploit failed: %v", err)
	}
	defer badResp.Body.Close()

	if badResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for unknown scenario, got %d", badResp.StatusCode)
	}
}

// TestServer_Concurrency verifies thread safety under parallel requests.
func TestServer_Concurrency(t *testing.T) {
	s := api.NewServer()
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	var wg sync.WaitGroup
	const workerCount = 20

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// Concurrently probe overview
			resp, err := http.Get(ts.URL + "/api/v1/overview")
			if err != nil {
				t.Errorf("worker %d overview failed: %v", id, err)
				return
			}
			resp.Body.Close()

			// Concurrently probe agents
			resp2, err := http.Get(ts.URL + "/api/v1/agents")
			if err != nil {
				t.Errorf("worker %d agents failed: %v", id, err)
				return
			}
			resp2.Body.Close()

			// Concurrently probe merkle
			resp3, err := http.Get(ts.URL + "/api/v1/ledger/merkle")
			if err != nil {
				t.Errorf("worker %d merkle failed: %v", id, err)
				return
			}
			resp3.Body.Close()
		}(i)
	}

	wg.Wait()
}
