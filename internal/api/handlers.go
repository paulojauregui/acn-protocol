package api

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/paulojauregui/acn-protocol/internal/escrow"
	"github.com/paulojauregui/acn-protocol/internal/governance"
	"github.com/paulojauregui/acn-protocol/internal/identity"
	"github.com/paulojauregui/acn-protocol/internal/ledger"
	"github.com/paulojauregui/acn-protocol/internal/oracle"
	"github.com/paulojauregui/acn-protocol/internal/relay"
	"github.com/paulojauregui/acn-protocol/internal/reputation"
	"github.com/paulojauregui/acn-protocol/internal/simulator"
)

// HealthResponse represents the standard liveness probe payload.
//
// Pre-conditions: none.
// Post-conditions: immutable serializable structure containing service health status.
type HealthResponse struct {
	Status  string `json:"status"`
	Service string `json:"service"`
	Version string `json:"version"`
}

// ReadyResponse represents the standard readiness probe payload.
//
// Pre-conditions: none.
// Post-conditions: reports readiness status along with active Merkle root and registered agent count.
type ReadyResponse struct {
	Status           string `json:"status"`
	MerkleRoot       string `json:"merkle_root"`
	RegisteredAgents int    `json:"registered_agents"`
}

// EscrowStats encapsulates aggregated escrow vault metrics.
//
// Pre-conditions: none.
// Post-conditions: monetary amounts are rounded to 2 decimal places.
type EscrowStats struct {
	TotalLockedUSD    float64 `json:"total_locked_usd"`
	TotalAvailableUSD float64 `json:"total_available_usd"`
	TreasuryFeesUSD   float64 `json:"treasury_fees_usd"`
	TotalSlashedUSD   float64 `json:"total_slashed_usd"`
}

// OverviewResponse represents the holistic network telemetry snapshot.
//
// Pre-conditions: none.
// Post-conditions: captures network state across identity, escrow, relay, and ledger subsystems.
type OverviewResponse struct {
	MerkleRoot        string                      `json:"merkle_root"`
	RegisteredAgents  int                         `json:"registered_agents"`
	ActiveSubAgents   int                         `json:"active_sub_agents"`
	Escrow            EscrowStats                 `json:"escrow"`
	RecentSessions    []*relay.NegotiationSession `json:"recent_sessions"`
	RecentOutcomes    []*oracle.SettlementOutcome `json:"recent_outcomes"`
	LedgerRecordCount int                         `json:"ledger_record_count"`
	ProposalCount     int                         `json:"proposal_count"`
}

// AgentItem combines a sovereign RootAgent record with its escrow balance and reputation metrics.
//
// Pre-conditions: Agent is non-nil.
// Post-conditions: point-in-time composite view of an individual agent.
type AgentItem struct {
	Agent              *identity.RootAgent  `json:"agent"`
	EscrowAccount      *escrow.BondAccount  `json:"escrow_account,omitempty"`
	ReputationSnapshot *reputation.Snapshot `json:"reputation_snapshot,omitempty"`
}

// AgentsResponse represents the list of all registered root agents and rotating sub-agents.
//
// Pre-conditions: none.
// Post-conditions: deterministic listing of active network participants.
type AgentsResponse struct {
	RootAgents []AgentItem          `json:"root_agents"`
	SubAgents  []*identity.SubAgent `json:"sub_agents"`
}

// AgentActionRequest specifies the parameters for registering, spawning, or depositing bond for agents.
//
// Pre-conditions: Action is one of "register_root", "spawn_sub", or "deposit_bond" (default "register_root").
// Post-conditions: value carrier deserialized from client POST requests.
type AgentActionRequest struct {
	Action         string  `json:"action"`
	DisplayName    string  `json:"display_name,omitempty"`
	PublicKeyHex   string  `json:"public_key_hex,omitempty"`
	PoPVerified    bool    `json:"pop_verified,omitempty"`
	TEEAttested    bool    `json:"tee_attested,omitempty"`
	IsVendor       bool    `json:"is_vendor,omitempty"`
	DepositBondUSD float64 `json:"deposit_bond_usd,omitempty"`
	RootDID        string  `json:"root_did,omitempty"`
	ParentRootDID  string  `json:"parent_root_did,omitempty"`
	Purpose        string  `json:"purpose,omitempty"`
	TTLSeconds     int     `json:"ttl_seconds,omitempty"`
	AmountUSD      float64 `json:"amount_usd,omitempty"`
}

// RelayBookRequest specifies parameters for initiating an Agent-to-Agent booking negotiation.
//
// Pre-conditions: CallerDID and VendorDID must be valid non-empty DIDs; BondAmountUSD > 0.
// Post-conditions: value carrier passed to Router.NegotiateBooking.
type RelayBookRequest struct {
	CallerDID         string  `json:"caller_did"`
	VendorDID         string  `json:"vendor_did"`
	ServiceName       string  `json:"service_name"`
	BondAmountUSD     float64 `json:"bond_amount_usd"`
	MinRequiredScore  float64 `json:"min_required_score"`
	RequiredTrustTier int     `json:"required_trust_tier"`
}

// OracleChallengeRequest specifies parameters for generating a physical presence challenge nonce.
//
// Pre-conditions: AppointmentID, UserDID, and VendorDID must be non-empty strings.
// Post-conditions: triggers generation of a 60-second single-use challenge nonce.
type OracleChallengeRequest struct {
	AppointmentID string `json:"appointment_id"`
	UserDID       string `json:"user_did"`
	VendorDID     string `json:"vendor_did"`
}

// OracleSettleRequest carries multi-factor settlement inputs for resolving an appointment.
//
// Pre-conditions: AppointmentID, Nonce, and VendorDID must be non-empty.
// Post-conditions: consumed during double-blind settlement to resolve escrow and reputation.
type OracleSettleRequest struct {
	AppointmentID       string  `json:"appointment_id"`
	LockID              string  `json:"lock_id"`
	Nonce               string  `json:"nonce"`
	UserRootDID         string  `json:"user_root_did"`
	UserDID             string  `json:"user_did"`
	VendorDID           string  `json:"vendor_did"`
	UserPresent         bool    `json:"user_present"`
	VendorPresent       bool    `json:"vendor_present"`
	StripePOSReceipt    bool    `json:"stripe_pos_receipt"`
	LocalBeaconVerified bool    `json:"local_beacon_verified"`
	PeerRating          float64 `json:"peer_rating"`
	AmountUSD           float64 `json:"amount_usd"`
}

// SettleResponse represents the outcome of an appointment settlement.
//
// Pre-conditions: none.
// Post-conditions: contains authoritative settlement outcome, escrow receipt, jury verdict, and updated Merkle root.
type SettleResponse struct {
	Outcome    *oracle.SettlementOutcome `json:"outcome"`
	Receipt    *escrow.SettlementReceipt `json:"receipt,omitempty"`
	Verdict    *governance.JuryVerdict   `json:"verdict,omitempty"`
	MerkleRoot string                    `json:"merkle_root"`
}

// PassportIssueRequest specifies requested predicate criteria for issuing a selective-disclosure TrustPassport.
//
// Pre-conditions: SubjectDID must be non-empty; threshold parameters must be non-negative.
// Post-conditions: value carrier for requesting enclave-signed credentials.
type PassportIssueRequest struct {
	SubjectDID        string  `json:"subject_did"`
	ReqMinFulfillment float64 `json:"req_min_fulfillment"`
	ReqMinBookings    int     `json:"req_min_bookings"`
	ReqMinTier        int     `json:"req_min_tier"`
}

// PassportVerifyRequest wraps a TrustPassport credential for verification.
//
// Pre-conditions: none.
// Post-conditions: supports both raw TrustPassport serialization or nested {passport: {...}} envelope.
type PassportVerifyRequest struct {
	Passport *ledger.TrustPassport `json:"passport,omitempty"`
	ledger.TrustPassport
}

// PassportVerifyResponse reports the validity of an audited TrustPassport.
//
// Pre-conditions: none.
// Post-conditions: Valid indicates whether the enclave signature and expiration check succeeded.
type PassportVerifyResponse struct {
	Valid      bool   `json:"valid"`
	PassportID string `json:"passport_id"`
	SubjectDID string `json:"subject_did"`
	MerkleRoot string `json:"merkle_root"`
}

// MerkleLedgerResponse encapsulates Merkle tree telemetry, leaves, and optional inclusion proof.
//
// Pre-conditions: none.
// Post-conditions: returns authoritative Merkle root and audit records.
type MerkleLedgerResponse struct {
	MerkleRoot     string                     `json:"merkle_root"`
	Records        []ledger.InteractionRecord `json:"records"`
	InclusionProof *ledger.InclusionProof     `json:"inclusion_proof,omitempty"`
}

// GovernanceProposalsResponse lists active proposals alongside the active protocol constitution.
//
// Pre-conditions: none.
// Post-conditions: provides comprehensive governance parameters and vote status.
type GovernanceProposalsResponse struct {
	Constitution governance.Constitution `json:"constitution"`
	Proposals    []*governance.Proposal  `json:"proposals"`
}

// VoteRequest carries an individual reputation-weighted vote submission.
//
// Pre-conditions: ProposalID and VoterDID must be non-empty strings.
// Post-conditions: value carrier deserialized from client vote POST requests.
type VoteRequest struct {
	ProposalID      string  `json:"proposal_id"`
	VoterDID        string  `json:"voter_did"`
	StakeUSD        float64 `json:"stake_usd"`
	ReputationScore float64 `json:"reputation_score"`
	Support         bool    `json:"support"`
	ValidatorVeto   bool    `json:"validator_veto"`
}

// VoteResponse captures the outcome of a governance vote submission.
//
// Pre-conditions: none.
// Post-conditions: returns immutable VoteReceipt and updated Proposal status.
type VoteResponse struct {
	Receipt  *governance.VoteReceipt `json:"receipt"`
	Proposal *governance.Proposal    `json:"proposal"`
}

// handleRoot serves the embedded HTML web console.
//
// Pre-conditions: s != nil; w != nil; r != nil.
// Post-conditions: serves uiIndexHTML with Content-Type text/html; charset=utf-8.
func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if len(uiIndexHTML) > 0 {
		_, _ = w.Write(uiIndexHTML)
	} else {
		_, _ = w.Write([]byte("<!DOCTYPE html><html><body><h1>ACN Protocol Console</h1></body></html>"))
	}
}

// handleHealthz responds to container liveness probes.
//
// Pre-conditions: s != nil; w != nil; r != nil.
// Post-conditions: returns HTTP 200 OK with HealthResponse JSON payload.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, HealthResponse{
		Status:  "healthy",
		Service: "acn-protocol",
		Version: "1.0.0",
	})
}

// handleReady responds to container readiness probes.
//
// Pre-conditions: s != nil; w != nil; r != nil.
// Post-conditions: returns HTTP 200 OK with current Merkle root and agent count.
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, ReadyResponse{
		Status:           "ready",
		MerkleRoot:       s.Ledger.CurrentMerkleRoot(),
		RegisteredAgents: len(s.Registry.ListRootAgents()),
	})
}

// handleOverview serves network-wide state, telemetry, and metrics.
//
// Pre-conditions: s != nil; w != nil; r != nil.
// Post-conditions: returns HTTP 200 OK with comprehensive OverviewResponse JSON.
func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	locked, avail, fees, slashed := s.Vault.Stats()
	writeJSON(w, http.StatusOK, OverviewResponse{
		MerkleRoot:       s.Ledger.CurrentMerkleRoot(),
		RegisteredAgents: len(s.Registry.ListRootAgents()),
		ActiveSubAgents:  len(s.Registry.ListSubAgents("")),
		Escrow: EscrowStats{
			TotalLockedUSD:    locked,
			TotalAvailableUSD: avail,
			TreasuryFeesUSD:   fees,
			TotalSlashedUSD:   slashed,
		},
		RecentSessions:    s.Router.ListSessions(),
		RecentOutcomes:    s.Oracle.ListOutcomes(),
		LedgerRecordCount: len(s.Ledger.ListRecords()),
		ProposalCount:     len(s.Governance.ListProposals()),
	})
}

// handleGetAgents returns all registered RootAgents and SubAgents.
//
// Pre-conditions: s != nil; w != nil; r != nil.
// Post-conditions: returns HTTP 200 OK with AgentsResponse JSON.
func (s *Server) handleGetAgents(w http.ResponseWriter, r *http.Request) {
	roots := s.Registry.ListRootAgents()
	rootItems := make([]AgentItem, 0, len(roots))

	for _, root := range roots {
		acc, _ := s.Vault.GetAccount(root.RootDID)
		snap, _ := s.Reputation.Evaluate(root.RootDID)
		rootItems = append(rootItems, AgentItem{
			Agent:              root,
			EscrowAccount:      acc,
			ReputationSnapshot: snap,
		})
	}

	subs := s.Registry.ListSubAgents("")
	writeJSON(w, http.StatusOK, AgentsResponse{
		RootAgents: rootItems,
		SubAgents:  subs,
	})
}

// handlePostAgents handles registration of Root Agents, Sub-Agent minting, or bond deposits.
//
// Pre-conditions: s != nil; w != nil; r != nil with valid AgentActionRequest JSON.
// Post-conditions: dispatches to appropriate identity or escrow operation and returns JSON result.
func (s *Server) handlePostAgents(w http.ResponseWriter, r *http.Request) {
	var req AgentActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request json: "+err.Error())
		return
	}

	action := strings.TrimSpace(req.Action)
	if action == "" || action == "register_root" {
		var pubKey []byte
		if req.PublicKeyHex != "" {
			decoded, err := hex.DecodeString(req.PublicKeyHex)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid public_key_hex: "+err.Error())
				return
			}
			pubKey = decoded
		}

		name := strings.TrimSpace(req.DisplayName)
		if name == "" {
			name = "Anonymous Agent"
		}

		root, err := s.Registry.RegisterRootAgent(name, pubKey, req.PoPVerified, req.TEEAttested, req.IsVendor)
		if err != nil {
			writeError(w, http.StatusBadRequest, "registration failed: "+err.Error())
			return
		}

		if req.DepositBondUSD > 0 {
			_, _ = s.Vault.DepositBond(root.RootDID, req.DepositBondUSD)
			root, _ = s.Registry.UpdateEconomics(root.RootDID, req.DepositBondUSD, 0, 0, -1)
			_, _ = s.Reputation.UpsertMetrics(reputation.Metrics{
				AgentDID: root.RootDID,
				StakeUSD: req.DepositBondUSD,
			})
		}

		writeJSON(w, http.StatusCreated, map[string]any{
			"status": "created",
			"agent":  root,
		})
		return
	}

	if action == "spawn_sub" {
		parentDID := strings.TrimSpace(req.RootDID)
		if parentDID == "" {
			parentDID = strings.TrimSpace(req.ParentRootDID)
		}
		if parentDID == "" {
			writeError(w, http.StatusBadRequest, "root_did is required to spawn sub-agent")
			return
		}

		purpose := strings.TrimSpace(req.Purpose)
		if purpose == "" {
			purpose = "general-delegation"
		}

		ttl := 24 * time.Hour
		if req.TTLSeconds > 0 {
			ttl = time.Duration(req.TTLSeconds) * time.Second
		}

		sub, err := s.Registry.SpawnSubAgent(parentDID, purpose, nil, ttl)
		if err != nil {
			writeError(w, http.StatusBadRequest, "spawn sub-agent failed: "+err.Error())
			return
		}

		writeJSON(w, http.StatusCreated, map[string]any{
			"status":    "spawned",
			"sub_agent": sub,
		})
		return
	}

	if action == "deposit_bond" {
		targetDID := strings.TrimSpace(req.RootDID)
		if targetDID == "" {
			targetDID = strings.TrimSpace(req.ParentRootDID)
		}
		if targetDID == "" {
			writeError(w, http.StatusBadRequest, "root_did is required for bond deposit")
			return
		}
		if req.AmountUSD <= 0 {
			writeError(w, http.StatusBadRequest, "amount_usd must be strictly positive")
			return
		}

		acc, err := s.Vault.DepositBond(targetDID, req.AmountUSD)
		if err != nil {
			writeError(w, http.StatusBadRequest, "deposit bond failed: "+err.Error())
			return
		}
		root, _ := s.Registry.UpdateEconomics(targetDID, req.AmountUSD, 0, 0, -1)

		writeJSON(w, http.StatusOK, map[string]any{
			"status":  "deposited",
			"account": acc,
			"agent":   root,
		})
		return
	}

	writeError(w, http.StatusBadRequest, "unknown action: "+action)
}

// handleRelayBook executes pre-answer trust gating and Agent-to-Agent booking negotiation.
//
// Pre-conditions: s != nil; w != nil; r != nil with valid RelayBookRequest JSON.
// Post-conditions: returns HTTP 200 OK with NegotiationSession JSON reflecting confirmed or rejected status.
func (s *Server) handleRelayBook(w http.ResponseWriter, r *http.Request) {
	var req RelayBookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request json: "+err.Error())
		return
	}

	session, err := s.Router.NegotiateBooking(relay.BookingRequest{
		CallerDID:         req.CallerDID,
		VendorDID:         req.VendorDID,
		ServiceName:       req.ServiceName,
		BondAmountUSD:     req.BondAmountUSD,
		MinRequiredScore:  req.MinRequiredScore,
		RequiredTrustTier: identity.TrustTier(req.RequiredTrustTier),
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "booking negotiation error: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, session)
}

// handleOracleChallenge issues a single-use 60-second physical presence challenge nonce.
//
// Pre-conditions: s != nil; w != nil; r != nil with valid OracleChallengeRequest JSON.
// Post-conditions: returns HTTP 201 Created with ChallengeNonce JSON payload.
func (s *Server) handleOracleChallenge(w http.ResponseWriter, r *http.Request) {
	var req OracleChallengeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request json: "+err.Error())
		return
	}

	nonce, err := s.Oracle.IssueChallengeNonce(req.AppointmentID, req.UserDID, req.VendorDID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to issue challenge nonce: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, nonce)
}

// handleOracleSettle executes double-blind settlement across oracle, escrow, and jury subsystems.
//
// Pre-conditions: s != nil; w != nil; r != nil with valid OracleSettleRequest JSON.
// Post-conditions: updates escrow collateral, adjudicates disputes, and appends record to Merkle ledger.
func (s *Server) handleOracleSettle(w http.ResponseWriter, r *http.Request) {
	var req OracleSettleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request json: "+err.Error())
		return
	}

	userRootDID := strings.TrimSpace(req.UserRootDID)
	if userRootDID == "" && req.UserDID != "" {
		if root, err := s.Registry.ResolveRootAgent(req.UserDID); err == nil && root != nil {
			userRootDID = root.RootDID
		} else {
			userRootDID = req.UserDID
		}
	}

	amountUSD := req.AmountUSD
	if amountUSD <= 0 && req.LockID != "" {
		if lock, err := s.Vault.GetLock(req.LockID); err == nil && lock != nil {
			amountUSD = lock.AmountUSD
		}
	}
	if amountUSD <= 0 {
		amountUSD = 10.0
	}

	outcome, err := s.Oracle.SettleAppointment(oracle.SettlementInput{
		AppointmentID:       req.AppointmentID,
		LockID:              req.LockID,
		Nonce:               req.Nonce,
		UserRootDID:         userRootDID,
		UserDID:             req.UserDID,
		VendorDID:           req.VendorDID,
		UserPresent:         req.UserPresent,
		VendorPresent:       req.VendorPresent,
		StripePOSReceipt:    req.StripePOSReceipt,
		LocalBeaconVerified: req.LocalBeaconVerified,
		PeerRating:          req.PeerRating,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "settlement failed: "+err.Error())
		return
	}

	var receipt *escrow.SettlementReceipt
	var verdict *governance.JuryVerdict
	merkleRoot := s.Ledger.CurrentMerkleRoot()

	switch outcome.Status {
	case oracle.StatusSuccess:
		if req.LockID != "" {
			receipt, _ = s.Vault.ReleaseBond(req.LockID)
		}
		_, _ = s.Registry.UpdateEconomics(userRootDID, 0, amountUSD, 1, -1)
		_, _ = s.Reputation.RecordSettlement(userRootDID, true, req.PeerRating, 24.0)
		_, _ = s.Reputation.RecordSettlement(req.VendorDID, true, req.PeerRating, 24.0)
		_, _ = s.Registry.UpdateEconomics(req.VendorDID, 0, 0, 1, -1)

		_, root, _ := s.Ledger.AppendRecord(ledger.InteractionRecord{
			AppointmentID: req.AppointmentID,
			UserRootDID:   userRootDID,
			SubAgentDID:   req.UserDID,
			VendorDID:     req.VendorDID,
			Status:        string(oracle.StatusSuccess),
			AmountUSD:     amountUSD,
			Timestamp:     time.Now().UTC(),
		})
		merkleRoot = root

	case oracle.StatusSlashed:
		if req.LockID != "" {
			receipt, _ = s.Vault.SlashBond(req.LockID, 0)
		}
		snap, _ := s.Reputation.RecordSettlement(userRootDID, false, 1.0, 0)
		newScore := 2.5
		if snap != nil {
			newScore = snap.CompositeScore
		}
		_, _ = s.Registry.RecordSlash(userRootDID, amountUSD, newScore)

		_, root, _ := s.Ledger.AppendRecord(ledger.InteractionRecord{
			AppointmentID: req.AppointmentID,
			UserRootDID:   userRootDID,
			SubAgentDID:   req.UserDID,
			VendorDID:     req.VendorDID,
			Status:        string(oracle.StatusSlashed),
			AmountUSD:     amountUSD,
			Timestamp:     time.Now().UTC(),
		})
		merkleRoot = root

	case oracle.StatusDisputed:
		userRep := 4.5
		if snap, err := s.Reputation.Evaluate(userRootDID); err == nil && snap != nil {
			userRep = snap.CompositeScore
		}
		disputeCase := governance.DisputeCase{
			CaseID:              "case-" + req.AppointmentID,
			AppointmentID:       req.AppointmentID,
			UserDID:             userRootDID,
			VendorDID:           req.VendorDID,
			UserClaimPresent:    req.UserPresent,
			VendorClaimPresent:  req.VendorPresent,
			StripePOSReceipt:    req.StripePOSReceipt,
			LocalBeaconVerified: req.LocalBeaconVerified,
			UserReputation:      userRep,
			VendorConflictCount: outcome.VendorConflictCount,
		}
		v, _ := s.Jury.ArbitrateDispute(r.Context(), disputeCase)
		verdict = v

		if verdict != nil && (verdict.Winner == governance.WinnerUser || !verdict.SlashUser) {
			if req.LockID != "" {
				receipt, _ = s.Vault.ReleaseBond(req.LockID)
			}
			_, _ = s.Reputation.RecordSettlement(userRootDID, true, 5.0, 24.0)
			_, _ = s.Registry.UpdateEconomics(userRootDID, 0, amountUSD, 1, -1)

			if verdict.SlashVendor {
				snap, _ := s.Reputation.RecordSettlement(req.VendorDID, false, 1.0, 0)
				vScore := 2.5
				if snap != nil {
					vScore = snap.CompositeScore
				}
				_, _ = s.Registry.RecordSlash(req.VendorDID, 0, vScore)
			}

			_, root, _ := s.Ledger.AppendRecord(ledger.InteractionRecord{
				AppointmentID: req.AppointmentID,
				UserRootDID:   userRootDID,
				SubAgentDID:   req.UserDID,
				VendorDID:     req.VendorDID,
				Status:        "STATUS_DISPUTED_RESOLVED_USER",
				AmountUSD:     amountUSD,
				Timestamp:     time.Now().UTC(),
			})
			merkleRoot = root
		} else {
			if req.LockID != "" {
				receipt, _ = s.Vault.SlashBond(req.LockID, 0)
			}
			snap, _ := s.Reputation.RecordSettlement(userRootDID, false, 1.0, 0)
			newScore := 2.5
			if snap != nil {
				newScore = snap.CompositeScore
			}
			_, _ = s.Registry.RecordSlash(userRootDID, amountUSD, newScore)

			_, root, _ := s.Ledger.AppendRecord(ledger.InteractionRecord{
				AppointmentID: req.AppointmentID,
				UserRootDID:   userRootDID,
				SubAgentDID:   req.UserDID,
				VendorDID:     req.VendorDID,
				Status:        "STATUS_DISPUTED_RESOLVED_VENDOR",
				AmountUSD:     amountUSD,
				Timestamp:     time.Now().UTC(),
			})
			merkleRoot = root
		}
	}

	writeJSON(w, http.StatusOK, SettleResponse{
		Outcome:    outcome,
		Receipt:    receipt,
		Verdict:    verdict,
		MerkleRoot: merkleRoot,
	})
}

// handleRaasPassport issues an enclave-signed selective-disclosure TrustPassport credential.
//
// Pre-conditions: s != nil; w != nil; r != nil with valid PassportIssueRequest JSON.
// Post-conditions: returns HTTP 200 OK with TrustPassport or HTTP 400 Bad Request if predicate fails.
func (s *Server) handleRaasPassport(w http.ResponseWriter, r *http.Request) {
	var req PassportIssueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request json: "+err.Error())
		return
	}

	if req.SubjectDID == "" {
		writeError(w, http.StatusBadRequest, "subject_did is required")
		return
	}

	root, err := s.Registry.ResolveRootAgent(req.SubjectDID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "subject agent not found: "+err.Error())
		return
	}

	actualFulfillment := 1.0
	actualBookings := root.SettledTxCount
	actualTier := int(root.TrustTier)
	actualScore := root.CompositeScore

	if snap, err := s.Reputation.Evaluate(root.RootDID); err == nil && snap != nil {
		actualFulfillment = snap.FulfillmentRate
		if snap.Metrics.SettledTxCount > actualBookings {
			actualBookings = snap.Metrics.SettledTxCount
		}
		actualScore = snap.CompositeScore
	}

	passport, err := s.Ledger.IssueTrustPassport(
		req.SubjectDID,
		actualFulfillment,
		actualBookings,
		actualTier,
		actualScore,
		req.ReqMinFulfillment,
		req.ReqMinBookings,
		req.ReqMinTier,
	)
	if err != nil {
		writeError(w, http.StatusBadRequest, "predicate evaluation failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, passport)
}

// handleRaasVerify verifies an enclave cryptographic attestation on a TrustPassport.
//
// Pre-conditions: s != nil; w != nil; r != nil with TrustPassport JSON.
// Post-conditions: returns HTTP 200 OK with PassportVerifyResponse JSON indicating validity.
func (s *Server) handleRaasVerify(w http.ResponseWriter, r *http.Request) {
	var req PassportVerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request json: "+err.Error())
		return
	}

	target := &req.TrustPassport
	if req.Passport != nil && req.Passport.PassportID != "" {
		target = req.Passport
	}

	valid := s.Ledger.VerifyTrustPassport(target)
	writeJSON(w, http.StatusOK, PassportVerifyResponse{
		Valid:      valid,
		PassportID: target.PassportID,
		SubjectDID: target.SubjectDID,
		MerkleRoot: target.MerkleRoot,
	})
}

// handleGetLedgerMerkle returns the current Merkle root, records, and optional inclusion proof.
//
// Pre-conditions: s != nil; w != nil; r != nil.
// Post-conditions: returns HTTP 200 OK with MerkleLedgerResponse JSON.
func (s *Server) handleGetLedgerMerkle(w http.ResponseWriter, r *http.Request) {
	root := s.Ledger.CurrentMerkleRoot()
	records := s.Ledger.ListRecords()

	var proof *ledger.InclusionProof
	indexParam := r.URL.Query().Get("index")
	if indexParam != "" {
		if idx, err := strconv.Atoi(indexParam); err == nil {
			if p, err := s.Ledger.GetInclusionProof(idx); err == nil {
				proof = p
			}
		}
	}

	writeJSON(w, http.StatusOK, MerkleLedgerResponse{
		MerkleRoot:     root,
		Records:        records,
		InclusionProof: proof,
	})
}

// handleGetGovernanceProposals lists all constitutional governance proposals.
//
// Pre-conditions: s != nil; w != nil; r != nil.
// Post-conditions: returns HTTP 200 OK with GovernanceProposalsResponse JSON.
func (s *Server) handleGetGovernanceProposals(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, GovernanceProposalsResponse{
		Constitution: s.Governance.GetConstitution(),
		Proposals:    s.Governance.ListProposals(),
	})
}

// handlePostGovernanceVote casts a reputation-weighted governance vote on an active proposal.
//
// Pre-conditions: s != nil; w != nil; r != nil with valid VoteRequest JSON.
// Post-conditions: returns HTTP 200 OK with VoteResponse JSON containing updated proposal state.
func (s *Server) handlePostGovernanceVote(w http.ResponseWriter, r *http.Request) {
	var req VoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request json: "+err.Error())
		return
	}

	if req.ProposalID == "" {
		writeError(w, http.StatusBadRequest, "proposal_id must not be empty")
		return
	}
	if req.VoterDID == "" {
		writeError(w, http.StatusBadRequest, "voter_did must not be empty")
		return
	}

	if req.StakeUSD <= 0 || req.ReputationScore <= 0 {
		if root, err := s.Registry.ResolveRootAgent(req.VoterDID); err == nil && root != nil {
			if req.StakeUSD <= 0 {
				req.StakeUSD = root.BondUSD
			}
			if req.ReputationScore <= 0 {
				req.ReputationScore = root.CompositeScore
			}
		}
	}

	receipt, prop, err := s.Governance.CastVote(
		req.ProposalID,
		req.VoterDID,
		req.StakeUSD,
		req.ReputationScore,
		req.Support,
		req.ValidatorVeto,
	)
	if err != nil {
		writeError(w, http.StatusBadRequest, "cast vote failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, VoteResponse{
		Receipt:  receipt,
		Proposal: prop,
	})
}

// handleSimulate executes adversarial threat simulation scenarios.
//
// Pre-conditions: s != nil; w != nil; r != nil with scenario matching sybil-swarm, whale-attack, or collusive-vendor.
// Post-conditions: returns HTTP 200 OK with SimulationResult JSON or HTTP 400 Bad Request on unknown scenario.
func (s *Server) handleSimulate(w http.ResponseWriter, r *http.Request) {
	scenario := strings.TrimSpace(r.PathValue("scenario"))
	if scenario == "" {
		trimmed := strings.TrimPrefix(r.URL.Path, "/api/v1/simulate")
		trimmed = strings.Trim(trimmed, "/")
		scenario = trimmed
	}

	switch scenario {
	case "sybil-swarm":
		res := simulator.RunSybilSwarmSimulation()
		writeJSON(w, http.StatusOK, res)
	case "whale-attack":
		res := simulator.RunWhaleAttackSimulation()
		writeJSON(w, http.StatusOK, res)
	case "collusive-vendor":
		res := simulator.RunCollusiveVendorSimulation()
		writeJSON(w, http.StatusOK, res)
	default:
		writeError(w, http.StatusBadRequest, "unknown simulation scenario: "+scenario)
	}
}

// writeJSON serializes data as JSON and writes standard response headers.
//
// Pre-conditions: w != nil; status is a valid HTTP status code.
// Post-conditions: sets Content-Type application/json; charset=utf-8 and writes serialized bytes.
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// writeError serializes an error message as JSON and sets error status code.
//
// Pre-conditions: w != nil; status >= 400.
// Post-conditions: writes JSON payload `{"error": msg}` with status code.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
