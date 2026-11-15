// Package api implements the HTTP/2 REST and Connect-style RPC control-plane
// server for the Agent Communication Network (ACN) protocol.
package api

import (
	"embed"
	"net/http"
	"time"

	"github.com/paulojauregui/acn-protocol/internal/escrow"
	"github.com/paulojauregui/acn-protocol/internal/governance"
	"github.com/paulojauregui/acn-protocol/internal/identity"
	"github.com/paulojauregui/acn-protocol/internal/ledger"
	"github.com/paulojauregui/acn-protocol/internal/oracle"
	"github.com/paulojauregui/acn-protocol/internal/relay"
	"github.com/paulojauregui/acn-protocol/internal/reputation"
)

// uiFS embeds every static asset under internal/api/ui (index.html, tokens.css,
// components.css, and any future stylesheets or scripts).
//
// Pre-conditions: the ui/ directory exists at build time alongside this package.
// Post-conditions: provides read-only access to embedded assets via uiFS.ReadFile.
//
//go:embed ui/*
var uiFS embed.FS

// Server is the HTTP/2 REST and Connect-style RPC control-plane server for the ACN protocol.
//
// Pre-conditions: initialized via NewServer.
// Post-conditions: provides concurrency-safe HTTP handling and access to domain engines.
type Server struct {
	Registry   *identity.Registry
	Reputation *reputation.Engine
	Vault      *escrow.Vault
	Oracle     *oracle.Oracle
	Ledger     *ledger.Ledger
	Governance *governance.Engine
	Jury       *governance.Jury
	Router     *relay.Router
	Mux        *http.ServeMux
}

// NewServer initializes all domain engines, registers route endpoints, and seeds the canonical network state.
//
// Pre-conditions: none.
// Post-conditions: returns a fully initialized Server with pre-seeded sovereign agents,
// merchant vendors, Merkle ledger records, and active governance proposals.
func NewServer() *Server {
	reg := identity.NewRegistry()
	rep := reputation.NewEngine()
	vault := escrow.NewVault()
	orc := oracle.NewOracle()
	led := ledger.NewLedger()
	gov := governance.NewEngine()
	jury := governance.NewJury("", false)
	router := relay.NewRouter(reg, rep, vault, led)
	mux := http.NewServeMux()

	s := &Server{
		Registry:   reg,
		Reputation: rep,
		Vault:      vault,
		Oracle:     orc,
		Ledger:     led,
		Governance: gov,
		Jury:       jury,
		Router:     router,
		Mux:        mux,
	}

	s.registerRoutes()
	s.seedInitialState()

	return s
}

// Handler returns the root http.Handler for the control-plane server.
//
// Pre-conditions: s is non-nil and initialized via NewServer.
// Post-conditions: returns the underlying http.ServeMux matching all registered routes.
func (s *Server) Handler() http.Handler {
	return s.Mux
}

// ServeHTTP dispatches incoming HTTP requests to the underlying ServeMux.
//
// Pre-conditions: s is non-nil; w and r are valid HTTP parameters.
// Post-conditions: routes the request according to configured endpoints.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.Mux.ServeHTTP(w, r)
}

// registerRoutes configures all REST and Connect-style RPC endpoint handlers.
//
// Pre-conditions: s is non-nil and s.Mux is initialized.
// Post-conditions: all protocol endpoints are registered on s.Mux.
func (s *Server) registerRoutes() {
	s.Mux.HandleFunc("GET /{$}", s.handleRoot)
	s.Mux.HandleFunc("GET /ui/{asset...}", s.handleUIAsset)
	s.Mux.HandleFunc("GET /healthz", s.handleHealthz)
	s.Mux.HandleFunc("GET /ready", s.handleReady)
	s.Mux.HandleFunc("GET /api/v1/overview", s.handleOverview)
	s.Mux.HandleFunc("GET /api/v1/agents", s.handleGetAgents)
	s.Mux.HandleFunc("POST /api/v1/agents", s.handlePostAgents)
	s.Mux.HandleFunc("POST /api/v1/relay/book", s.handleRelayBook)
	s.Mux.HandleFunc("POST /api/v1/oracle/challenge", s.handleOracleChallenge)
	s.Mux.HandleFunc("POST /api/v1/oracle/settle", s.handleOracleSettle)
	s.Mux.HandleFunc("POST /api/v1/raas/passport", s.handleRaasPassport)
	s.Mux.HandleFunc("POST /api/v1/raas/verify", s.handleRaasVerify)
	s.Mux.HandleFunc("GET /api/v1/ledger/merkle", s.handleGetLedgerMerkle)
	s.Mux.HandleFunc("GET /api/v1/governance/proposals", s.handleGetGovernanceProposals)
	s.Mux.HandleFunc("POST /api/v1/governance/vote", s.handlePostGovernanceVote)
	s.Mux.HandleFunc("POST /api/v1/simulate/{scenario}", s.handleSimulate)
	s.Mux.HandleFunc("POST /api/v1/simulate", s.handleSimulate)
}

// seedInitialState populates the realistic initial network state specified in M10_SPEC.md.
//
// Pre-conditions: s is non-nil with initialized domain engines.
// Post-conditions: pre-seeds Sovereign User Agents (Tiers 0-3), Rotating Sub-Agents,
// Merchant Vendors (Waymo, UCSF, Michelin), settled Merkle records, and governance proposals.
func (s *Server) seedInitialState() {
	// 1. Sovereign User Agents across Tiers 0-3
	// Alice Sovereign Executive (Tier 3 Sovereign, PoPVerified: true, TEEAttested: true, BondUSD: 500, CumulativeSpendUSD: 2400, SettledTxCount: 35, CompositeScore: 4.85)
	aliceRoot, err := s.Registry.RegisterRootAgent("Alice Sovereign Executive", nil, true, true, false)
	if err == nil && aliceRoot != nil {
		_, _ = s.Vault.DepositBond(aliceRoot.RootDID, 500.0)
		_, _ = s.Vault.RecordStripeSpend(aliceRoot.RootDID, 2400.0, 35)
		_, _ = s.Registry.UpdateEconomics(aliceRoot.RootDID, 500.0, 2400.0, 35, 4.85)
		_, _ = s.Reputation.UpsertMetrics(reputation.Metrics{
			AgentDID:               aliceRoot.RootDID,
			FulfilledCount:         35,
			NoShowCount:            0,
			AvgCancelLeadTimeHours: 24.0,
			PeerReviewAvg:          4.90,
			StakeAgeDays:           180.0,
			StakeUSD:               500.0,
			AccountAgeDays:         180.0,
			SettledTxCount:         35,
		})
		// 2 rotating Sub-Agents for Alice
		_, _ = s.Registry.SpawnSubAgent(aliceRoot.RootDID, "autonomous-ride-hailing", nil, 24*time.Hour)
		_, _ = s.Registry.SpawnSubAgent(aliceRoot.RootDID, "executive-medical-concierge", nil, 24*time.Hour)
	}

	// Bob Reputational Traveler (Tier 2 Reputational, BondUSD: 150, CumulativeSpendUSD: 750, SettledTxCount: 22, CompositeScore: 4.10)
	bobRoot, err := s.Registry.RegisterRootAgent("Bob Reputational Traveler", nil, false, false, false)
	if err == nil && bobRoot != nil {
		_, _ = s.Vault.DepositBond(bobRoot.RootDID, 150.0)
		_, _ = s.Vault.RecordStripeSpend(bobRoot.RootDID, 750.0, 22)
		_, _ = s.Registry.UpdateEconomics(bobRoot.RootDID, 150.0, 750.0, 22, 4.10)
		_, _ = s.Reputation.UpsertMetrics(reputation.Metrics{
			AgentDID:               bobRoot.RootDID,
			FulfilledCount:         22,
			NoShowCount:            0,
			AvgCancelLeadTimeHours: 20.0,
			PeerReviewAvg:          4.10,
			StakeAgeDays:           90.0,
			StakeUSD:               150.0,
			AccountAgeDays:         90.0,
			SettledTxCount:         22,
		})
		// 1 rotating Sub-Agent for Bob
		_, _ = s.Registry.SpawnSubAgent(bobRoot.RootDID, "travel-flight-hotel", nil, 24*time.Hour)
	}

	// Charlie Economic Commuter (Tier 1 Economic, BondUSD: 60, CumulativeSpendUSD: 120, SettledTxCount: 6, CompositeScore: 3.40)
	charlieRoot, err := s.Registry.RegisterRootAgent("Charlie Economic Commuter", nil, false, false, false)
	if err == nil && charlieRoot != nil {
		_, _ = s.Vault.DepositBond(charlieRoot.RootDID, 60.0)
		_, _ = s.Vault.RecordStripeSpend(charlieRoot.RootDID, 120.0, 6)
		_, _ = s.Registry.UpdateEconomics(charlieRoot.RootDID, 60.0, 120.0, 6, 3.40)
		_, _ = s.Reputation.UpsertMetrics(reputation.Metrics{
			AgentDID:               charlieRoot.RootDID,
			FulfilledCount:         6,
			NoShowCount:            0,
			AvgCancelLeadTimeHours: 12.0,
			PeerReviewAvg:          3.50,
			StakeAgeDays:           30.0,
			StakeUSD:               60.0,
			AccountAgeDays:         30.0,
			SettledTxCount:         6,
		})
	}

	// Eve Unbonded Ephemeral Bot (Tier 0 Ephemeral, BondUSD: 0, CumulativeSpendUSD: 0, SettledTxCount: 0, CompositeScore: 2.10)
	eveRoot, err := s.Registry.RegisterRootAgent("Eve Unbonded Ephemeral Bot", nil, false, false, false)
	if err == nil && eveRoot != nil {
		_, _ = s.Registry.UpdateEconomics(eveRoot.RootDID, 0.0, 0.0, 0, 2.10)
		_, _ = s.Reputation.UpsertMetrics(reputation.Metrics{
			AgentDID:               eveRoot.RootDID,
			FulfilledCount:         0,
			NoShowCount:            1,
			AvgCancelLeadTimeHours: 0.0,
			PeerReviewAvg:          2.10,
			StakeAgeDays:           0.0,
			StakeUSD:               0.0,
			AccountAgeDays:         1.0,
			SettledTxCount:         0,
		})
	}

	// 2. Merchant Vendor Agents
	// Autonomous Waymo Fleet SF (IsVendor: true, BondUSD: 5000, CompositeScore: 4.92)
	waymoRoot, _ := s.Registry.RegisterRootAgent("Autonomous Waymo Fleet SF", nil, true, true, true)
	if waymoRoot != nil {
		_, _ = s.Vault.DepositBond(waymoRoot.RootDID, 5000.0)
		_, _ = s.Vault.RecordStripeSpend(waymoRoot.RootDID, 150000.0, 1250)
		_, _ = s.Registry.UpdateEconomics(waymoRoot.RootDID, 5000.0, 150000.0, 1250, 4.92)
		_, _ = s.Reputation.UpsertMetrics(reputation.Metrics{
			AgentDID:               waymoRoot.RootDID,
			FulfilledCount:         1250,
			NoShowCount:            0,
			AvgCancelLeadTimeHours: 24.0,
			PeerReviewAvg:          4.95,
			StakeAgeDays:           365.0,
			StakeUSD:               5000.0,
			AccountAgeDays:         365.0,
			SettledTxCount:         1250,
		})
	}

	// UCSF Medical Screening (IsVendor: true, BondUSD: 10000, CompositeScore: 4.96)
	ucsfRoot, _ := s.Registry.RegisterRootAgent("UCSF Medical Screening", nil, true, true, true)
	if ucsfRoot != nil {
		_, _ = s.Vault.DepositBond(ucsfRoot.RootDID, 10000.0)
		_, _ = s.Vault.RecordStripeSpend(ucsfRoot.RootDID, 300000.0, 2100)
		_, _ = s.Registry.UpdateEconomics(ucsfRoot.RootDID, 10000.0, 300000.0, 2100, 4.96)
		_, _ = s.Reputation.UpsertMetrics(reputation.Metrics{
			AgentDID:               ucsfRoot.RootDID,
			FulfilledCount:         2100,
			NoShowCount:            0,
			AvgCancelLeadTimeHours: 24.0,
			PeerReviewAvg:          4.98,
			StakeAgeDays:           500.0,
			StakeUSD:               10000.0,
			AccountAgeDays:         500.0,
			SettledTxCount:         2100,
		})
	}

	// Michelin Dining Agent (IsVendor: true, BondUSD: 2500, CompositeScore: 4.88)
	michelinRoot, _ := s.Registry.RegisterRootAgent("Michelin Dining Agent", nil, true, true, true)
	if michelinRoot != nil {
		_, _ = s.Vault.DepositBond(michelinRoot.RootDID, 2500.0)
		_, _ = s.Vault.RecordStripeSpend(michelinRoot.RootDID, 80000.0, 640)
		_, _ = s.Registry.UpdateEconomics(michelinRoot.RootDID, 2500.0, 80000.0, 640, 4.88)
		_, _ = s.Reputation.UpsertMetrics(reputation.Metrics{
			AgentDID:               michelinRoot.RootDID,
			FulfilledCount:         640,
			NoShowCount:            0,
			AvgCancelLeadTimeHours: 24.0,
			PeerReviewAvg:          4.90,
			StakeAgeDays:           200.0,
			StakeUSD:               2500.0,
			AccountAgeDays:         200.0,
			SettledTxCount:         640,
		})
	}

	// 3. Initial Settled Merkle Ledger Records
	if aliceRoot != nil && waymoRoot != nil {
		_, _, _ = s.Ledger.AppendRecord(ledger.InteractionRecord{
			RecordID:      "rec-seed-001",
			AppointmentID: "apt-seed-001",
			UserRootDID:   aliceRoot.RootDID,
			SubAgentDID:   aliceRoot.RootDID,
			VendorDID:     waymoRoot.RootDID,
			Status:        "STATUS_SUCCESS",
			AmountUSD:     45.00,
			Timestamp:     time.Now().UTC().Add(-72 * time.Hour),
		})
	}
	if aliceRoot != nil && michelinRoot != nil {
		_, _, _ = s.Ledger.AppendRecord(ledger.InteractionRecord{
			RecordID:      "rec-seed-002",
			AppointmentID: "apt-seed-002",
			UserRootDID:   aliceRoot.RootDID,
			SubAgentDID:   aliceRoot.RootDID,
			VendorDID:     michelinRoot.RootDID,
			Status:        "STATUS_SUCCESS",
			AmountUSD:     250.00,
			Timestamp:     time.Now().UTC().Add(-48 * time.Hour),
		})
	}
	if bobRoot != nil && waymoRoot != nil {
		_, _, _ = s.Ledger.AppendRecord(ledger.InteractionRecord{
			RecordID:      "rec-seed-003",
			AppointmentID: "apt-seed-003",
			UserRootDID:   bobRoot.RootDID,
			SubAgentDID:   bobRoot.RootDID,
			VendorDID:     waymoRoot.RootDID,
			Status:        "STATUS_SUCCESS",
			AmountUSD:     35.00,
			Timestamp:     time.Now().UTC().Add(-24 * time.Hour),
		})
	}
	if charlieRoot != nil && waymoRoot != nil {
		_, _, _ = s.Ledger.AppendRecord(ledger.InteractionRecord{
			RecordID:      "rec-seed-004",
			AppointmentID: "apt-seed-004",
			UserRootDID:   charlieRoot.RootDID,
			SubAgentDID:   charlieRoot.RootDID,
			VendorDID:     waymoRoot.RootDID,
			Status:        "STATUS_SUCCESS",
			AmountUSD:     25.00,
			Timestamp:     time.Now().UTC().Add(-12 * time.Hour),
		})
	}

	// 4. Initial Governance Proposals
	_, _ = s.Governance.CreateProposal(
		"prop-001",
		"Adjust Network Integrity Fee Rate",
		"Proposal to adjust the protocol network integrity fee rate to 0.004 (0.4%) to stimulate micro-transaction volume.",
		"network_integrity_fee_rate",
		"0.004",
	)
	_, _ = s.Governance.CreateProposal(
		"prop-002",
		"Increase Default Challenge Nonce TTL",
		"Proposal to increase DefaultNonceTTLSeconds from 60 seconds to 90 seconds for edge network latency tolerance.",
		"nonce_ttl_seconds",
		"90",
	)
}
