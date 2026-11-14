// Package relay implements the high-concurrency Agent Relay and SIP-style
// Agent-to-Agent Call & Booking Negotiation state machine for the ACN protocol.
//
// The state machine coordinates pre-answer trust gating, selective-disclosure
// TrustPassport issuance, and blind escrow locking:
//
//	INITIATED -> REPUTATION_VERIFIED -> ESCROW_LOCKED -> CONFIRMED
//	      \               \                   \
//	       +---------------+-------------------+---> REJECTED_LOW_TRUST
//
// Rotating Sub-Agents derived from sovereign Root Agents seamlessly inherit
// their parent's trust tier, reputation score, and Good-Behavior Bond vault
// while presenting an unlinkable ephemeral identity to the vendor.
package relay

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/paulojauregui/acn-protocol/internal/escrow"
	"github.com/paulojauregui/acn-protocol/internal/identity"
	"github.com/paulojauregui/acn-protocol/internal/ledger"
	"github.com/paulojauregui/acn-protocol/internal/reputation"
)

// SessionState represents the lifecycle status of an Agent-to-Agent booking negotiation session.
//
// Pre-conditions: none.
// Post-conditions: valid values are StateInitiated, StateReputationVerified, StateEscrowLocked,
// StateConfirmed, or StateRejectedLowTrust.
type SessionState string

const (
	// StateInitiated indicates that the negotiation session has been created.
	//
	// Pre-conditions: none.
	// Post-conditions: constant string value "INITIATED".
	StateInitiated SessionState = "INITIATED"

	// StateReputationVerified indicates that caller reputation and trust tier requirements have been verified.
	//
	// Pre-conditions: none.
	// Post-conditions: constant string value "REPUTATION_VERIFIED".
	StateReputationVerified SessionState = "REPUTATION_VERIFIED"

	// StateEscrowLocked indicates that the required commitment bond has been locked in escrow.
	//
	// Pre-conditions: none.
	// Post-conditions: constant string value "ESCROW_LOCKED".
	StateEscrowLocked SessionState = "ESCROW_LOCKED"

	// StateConfirmed indicates that the negotiation session and booking have completed successfully.
	//
	// Pre-conditions: none.
	// Post-conditions: constant string value "CONFIRMED".
	StateConfirmed SessionState = "CONFIRMED"

	// StateRejectedLowTrust indicates that the booking was rejected due to insufficient trust, tier, velocity, or bond.
	//
	// Pre-conditions: none.
	// Post-conditions: constant string value "REJECTED_LOW_TRUST".
	StateRejectedLowTrust SessionState = "REJECTED_LOW_TRUST"
)

// Sentinel errors returned by the relay Router.
//
// Pre-conditions: none (package-level immutable sentinels).
// Post-conditions: error values for classification via errors.Is.
var (
	// ErrInvalidCallerDID indicates that the caller DID is empty.
	//
	// Pre-conditions: none.
	// Post-conditions: immutable sentinel error.
	ErrInvalidCallerDID = errors.New("relay: caller did cannot be empty")

	// ErrInvalidVendorDID indicates that the vendor DID is empty.
	//
	// Pre-conditions: none.
	// Post-conditions: immutable sentinel error.
	ErrInvalidVendorDID = errors.New("relay: vendor did cannot be empty")

	// ErrNilDependency indicates that a required subsystem dependency was nil.
	//
	// Pre-conditions: none.
	// Post-conditions: immutable sentinel error.
	ErrNilDependency = errors.New("relay: nil subsystem dependency")

	// ErrSessionNotFound indicates that the requested session does not exist.
	//
	// Pre-conditions: none.
	// Post-conditions: immutable sentinel error.
	ErrSessionNotFound = errors.New("relay: session not found")
)

// BookingRequest specifies the parameters for an Agent-to-Agent booking negotiation.
//
// Pre-conditions: CallerDID and VendorDID must be valid non-empty DIDs; BondAmountUSD must be positive.
// Post-conditions: value carrier for initiating a booking negotiation via NegotiateBooking.
type BookingRequest struct {
	CallerDID         string             `json:"caller_did"`
	VendorDID         string             `json:"vendor_did"`
	ServiceName       string             `json:"service_name"`
	BondAmountUSD     float64            `json:"bond_amount_usd"`
	MinRequiredScore  float64            `json:"min_required_score"`
	RequiredTrustTier identity.TrustTier `json:"required_trust_tier"`
}

// NegotiationSession represents the state and provenance of an Agent-to-Agent negotiation.
//
// Pre-conditions: constructed via Router.NegotiateBooking.
// Post-conditions: captures the end-to-end audit trail including state transitions, lock IDs, and passport IDs.
type NegotiationSession struct {
	SessionID       string             `json:"session_id"`
	AppointmentID   string             `json:"appointment_id"`
	CallerDID       string             `json:"caller_did"`
	CallerRootDID   string             `json:"caller_root_did"`
	VendorDID       string             `json:"vendor_did"`
	ServiceName     string             `json:"service_name"`
	BondAmountUSD   float64            `json:"bond_amount_usd"`
	CallerScore     float64            `json:"caller_score"`
	CallerTier      identity.TrustTier `json:"caller_tier"`
	State           SessionState       `json:"state"`
	StateHistory    []SessionState     `json:"state_history"`
	LockID          string             `json:"lock_id"`
	PassportID      string             `json:"passport_id"`
	RejectionReason string             `json:"rejection_reason"`
	CreatedAt       time.Time          `json:"created_at"`
}

// Router orchestrates SIP-style Agent-to-Agent call and booking negotiations across
// identity, reputation, escrow, and ledger subsystems.
//
// Pre-conditions: constructed via NewRouter with non-nil dependencies.
// Post-conditions: provides concurrency-safe booking negotiation and session retrieval.
type Router struct {
	mu       sync.RWMutex
	reg      *identity.Registry
	rep      *reputation.Engine
	vault    *escrow.Vault
	led      *ledger.Ledger
	sessions map[string]*NegotiationSession
}

// NewRouter instantiates a new Router with references to identity, reputation, escrow, and ledger subsystems.
//
// Pre-conditions: reg, rep, vault, and led must be non-nil initialized subsystems.
// Post-conditions: returns an operational *Router ready to handle concurrent booking negotiations.
func NewRouter(reg *identity.Registry, rep *reputation.Engine, vault *escrow.Vault, led *ledger.Ledger) *Router {
	return &Router{
		reg:      reg,
		rep:      rep,
		vault:    vault,
		led:      led,
		sessions: make(map[string]*NegotiationSession),
	}
}

// NegotiateBooking executes the SIP-style state machine for an Agent-to-Agent booking negotiation.
//
// Pre-conditions: req must contain valid non-empty CallerDID and VendorDID.
// Post-conditions: returns a NegotiationSession in StateConfirmed if all checks pass and escrow is locked;
// returns a session in StateRejectedLowTrust with RejectionReason populated if trust, tier, velocity, or bond checks fail.
func (r *Router) NegotiateBooking(req BookingRequest) (*NegotiationSession, error) {
	if r.reg == nil || r.vault == nil || r.led == nil {
		return nil, ErrNilDependency
	}
	if req.CallerDID == "" {
		return nil, ErrInvalidCallerDID
	}
	if req.VendorDID == "" {
		return nil, ErrInvalidVendorDID
	}

	sessionID := generateID("sess_")
	appointmentID := generateID("apt_")
	now := time.Now().UTC()

	session := &NegotiationSession{
		SessionID:     sessionID,
		AppointmentID: appointmentID,
		CallerDID:     req.CallerDID,
		VendorDID:     req.VendorDID,
		ServiceName:   req.ServiceName,
		BondAmountUSD: req.BondAmountUSD,
		State:         StateInitiated,
		StateHistory:  []SessionState{StateInitiated},
		CreatedAt:     now,
	}

	// 1. Resolve Root Agent (rotating Sub-Agents inherit parent Root Agent's tier, score, and bond vault)
	root, err := r.reg.ResolveRootAgent(req.CallerDID)
	if err != nil {
		session.State = StateRejectedLowTrust
		session.StateHistory = append(session.StateHistory, StateRejectedLowTrust)
		session.RejectionReason = fmt.Sprintf("failed to resolve caller agent: %v", err)
		r.recordSession(session)
		return cloneSession(session), nil
	}

	session.CallerRootDID = root.RootDID
	session.CallerTier = root.TrustTier

	// 2. Evaluate caller reputation & velocity via reputation engine (fallback to root.CompositeScore)
	callerScore := root.CompositeScore
	var snap *reputation.Snapshot
	if r.rep != nil {
		evalSnap, evalErr := r.rep.Evaluate(root.RootDID)
		if evalErr == nil && evalSnap != nil {
			snap = evalSnap
			callerScore = evalSnap.CompositeScore
		}
	}
	session.CallerScore = callerScore

	// 3. Verify trust, tier, exposure, and velocity gates
	// Gate 1: Caller score >= MinRequiredScore
	if callerScore < req.MinRequiredScore {
		session.State = StateRejectedLowTrust
		session.StateHistory = append(session.StateHistory, StateRejectedLowTrust)
		session.RejectionReason = fmt.Sprintf("caller score %.2f is below required minimum %.2f", callerScore, req.MinRequiredScore)
		r.recordSession(session)
		return cloneSession(session), nil
	}

	// Gate 2: Root TrustTier >= RequiredTrustTier
	if root.TrustTier < req.RequiredTrustTier {
		session.State = StateRejectedLowTrust
		session.StateHistory = append(session.StateHistory, StateRejectedLowTrust)
		session.RejectionReason = fmt.Sprintf("caller trust tier %s is below required tier %s", root.TrustTier, req.RequiredTrustTier)
		r.recordSession(session)
		return cloneSession(session), nil
	}

	// Gate 3: BondAmountUSD <= root.TrustTier.MaxExposureUSD()
	maxExposure := root.TrustTier.MaxExposureUSD()
	if req.BondAmountUSD > maxExposure {
		session.State = StateRejectedLowTrust
		session.StateHistory = append(session.StateHistory, StateRejectedLowTrust)
		session.RejectionReason = fmt.Sprintf("bond amount %.2f USD exceeds trust tier %s maximum exposure of %.2f USD", req.BondAmountUSD, root.TrustTier, maxExposure)
		r.recordSession(session)
		return cloneSession(session), nil
	}

	// Gate 4: Velocity check
	if snap != nil {
		// Whale defense: newly created account with zero age and zero settled transactions has zero velocity capacity
		if snap.Metrics.StakeUSD > 0 && snap.Metrics.AccountAgeDays == 0 && snap.Metrics.SettledTxCount == 0 {
			session.State = StateRejectedLowTrust
			session.StateHistory = append(session.StateHistory, StateRejectedLowTrust)
			session.RejectionReason = "zero velocity capacity for newly created unseasoned account"
			r.recordSession(session)
			return cloneSession(session), nil
		}
		if snap.VelocityCapacityUSD > 0 && req.BondAmountUSD > snap.VelocityCapacityUSD {
			session.State = StateRejectedLowTrust
			session.StateHistory = append(session.StateHistory, StateRejectedLowTrust)
			session.RejectionReason = fmt.Sprintf("bond amount %.2f USD exceeds velocity capacity of %.2f USD", req.BondAmountUSD, snap.VelocityCapacityUSD)
			r.recordSession(session)
			return cloneSession(session), nil
		}
	}

	// 4. Issue selective-disclosure TrustPassport
	actualFulfillment := 1.0
	actualSettled := root.SettledTxCount
	if snap != nil {
		actualFulfillment = snap.FulfillmentRate
		if snap.Metrics.SettledTxCount > actualSettled {
			actualSettled = snap.Metrics.SettledTxCount
		}
	}

	passport, err := r.led.IssueTrustPassport(
		req.CallerDID,
		actualFulfillment,
		actualSettled,
		int(root.TrustTier),
		callerScore,
		0.0,
		0,
		int(req.RequiredTrustTier),
	)
	if err != nil {
		session.State = StateRejectedLowTrust
		session.StateHistory = append(session.StateHistory, StateRejectedLowTrust)
		session.RejectionReason = fmt.Sprintf("failed to issue trust passport: %v", err)
		r.recordSession(session)
		return cloneSession(session), nil
	}

	session.PassportID = passport.PassportID
	session.State = StateReputationVerified
	session.StateHistory = append(session.StateHistory, StateReputationVerified)

	// 5. Lock bond in blind escrow vault
	lock, err := r.vault.LockBond(appointmentID, root.RootDID, req.CallerDID, req.VendorDID, req.BondAmountUSD)
	if err != nil {
		session.State = StateRejectedLowTrust
		session.StateHistory = append(session.StateHistory, StateRejectedLowTrust)
		session.RejectionReason = "insufficient available escrow bond"
		r.recordSession(session)
		return cloneSession(session), nil
	}

	session.LockID = lock.LockID
	session.State = StateEscrowLocked
	session.StateHistory = append(session.StateHistory, StateEscrowLocked)

	// 6. Confirm session
	session.State = StateConfirmed
	session.StateHistory = append(session.StateHistory, StateConfirmed)

	r.recordSession(session)
	return cloneSession(session), nil
}

// ListSessions returns a deterministic snapshot of all negotiated sessions ordered by creation time.
//
// Pre-conditions: none.
// Post-conditions: returns an ordered slice of cloned NegotiationSession objects; safe for concurrent callers.
func (r *Router) ListSessions() []*NegotiationSession {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]*NegotiationSession, 0, len(r.sessions))
	for _, sess := range r.sessions {
		out = append(out, cloneSession(sess))
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].SessionID < out[j].SessionID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})

	return out
}

// GetSession retrieves a negotiation session by its unique session identifier.
//
// Pre-conditions: sessionID is non-empty.
// Post-conditions: returns a deep copy of the session if found; otherwise returns nil and ErrSessionNotFound.
func (r *Router) GetSession(sessionID string) (*NegotiationSession, error) {
	if sessionID == "" {
		return nil, ErrSessionNotFound
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	sess, exists := r.sessions[sessionID]
	if !exists {
		return nil, ErrSessionNotFound
	}
	return cloneSession(sess), nil
}

// recordSession securely stores a deep copy of a NegotiationSession in the router state.
//
// Pre-conditions: s is non-nil with a valid SessionID.
// Post-conditions: session is inserted or replaced under write lock.
func (r *Router) recordSession(s *NegotiationSession) {
	if s == nil || s.SessionID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[s.SessionID] = cloneSession(s)
}

// cloneSession creates a deep copy of a NegotiationSession.
//
// Pre-conditions: s may be nil or populated.
// Post-conditions: returns an independent copy with cloned StateHistory slice.
func cloneSession(s *NegotiationSession) *NegotiationSession {
	if s == nil {
		return nil
	}
	c := *s
	if s.StateHistory != nil {
		c.StateHistory = make([]SessionState, len(s.StateHistory))
		copy(c.StateHistory, s.StateHistory)
	}
	return &c
}

// generateID produces a secure random identifier with the specified prefix.
//
// Pre-conditions: prefix is non-empty.
// Post-conditions: returns a collision-resistant identifier formatted as prefix + hex-encoded random bytes.
func generateID(prefix string) string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		h := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", prefix, time.Now().UnixNano())))
		return prefix + hex.EncodeToString(h[:16])
	}
	return prefix + hex.EncodeToString(b)
}
