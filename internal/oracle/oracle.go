package oracle

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Sentinel errors returned by the Oracle.
//
// Pre-conditions: none.
// Post-conditions: every Oracle error wraps one of these sentinels so callers can use
// errors.Is for classification.
var (
	// ErrEmptyAppointmentID indicates a blank appointment identifier was provided.
	ErrEmptyAppointmentID = errors.New("oracle: appointment id must not be empty")

	// ErrEmptyUserDID indicates a blank user DID was provided.
	ErrEmptyUserDID = errors.New("oracle: user did must not be empty")

	// ErrEmptyVendorDID indicates a blank vendor DID was provided.
	ErrEmptyVendorDID = errors.New("oracle: vendor did must not be empty")

	// ErrNonceNotFound indicates the requested challenge nonce does not exist in the registry.
	ErrNonceNotFound = errors.New("oracle: challenge nonce not found")

	// ErrNonceAppointmentMismatch indicates the nonce was issued for a different appointment ID.
	ErrNonceAppointmentMismatch = errors.New("oracle: challenge nonce does not match appointment id")

	// ErrNonceConsumed indicates the challenge nonce was already consumed (replay attack detected).
	ErrNonceConsumed = errors.New("oracle: challenge nonce has already been consumed")

	// ErrNonceExpired indicates the challenge nonce 60-second TTL has elapsed.
	ErrNonceExpired = errors.New("oracle: challenge nonce has expired")

	// ErrOutcomeNotFound indicates no settlement outcome exists for the requested appointment ID.
	ErrOutcomeNotFound = errors.New("oracle: settlement outcome not found")

	// ErrAlreadySettled indicates the appointment has already been finalized and settled.
	ErrAlreadySettled = errors.New("oracle: appointment has already been settled")
)

// SettlementStatus represents the categorical resolution of an appointment settlement.
//
// Pre-conditions: must be one of StatusSuccess, StatusSlashed, or StatusDisputed.
// Post-conditions: drives downstream escrow fund distribution and reputation adjustments.
type SettlementStatus string

const (
	// StatusSuccess indicates mutual attestation of physical presence; funds are released.
	StatusSuccess SettlementStatus = "STATUS_SUCCESS"

	// StatusSlashed indicates confirmed user absence (no-show); user commitment bond is slashed.
	StatusSlashed SettlementStatus = "STATUS_SLASHED"

	// StatusDisputed indicates conflicting arrival claims between user and vendor.
	StatusDisputed SettlementStatus = "STATUS_DISPUTED"
)

// SettlementInput carries all attestation, cryptographic, and third-party hardware
// telemetry required by the Oracle to settle an appointment.
//
// Pre-conditions: AppointmentID, Nonce, and VendorDID must be populated.
// Post-conditions: consumed during SettleAppointment to determine the authoritative SettlementOutcome.
type SettlementInput struct {
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
}

// SettlementOutcome records the authoritative, immutable resolution of an appointment
// check-in decided by the Oracle.
//
// Pre-conditions: produced exclusively by Oracle.SettleAppointment.
// Post-conditions: safe for concurrent reads; immutable record of slashing penalties and rewards.
type SettlementOutcome struct {
	AppointmentID           string           `json:"appointment_id"`
	LockID                  string           `json:"lock_id"`
	Nonce                   string           `json:"nonce"`
	Status                  SettlementStatus `json:"status"`
	UserSlashed             bool             `json:"user_slashed"`
	VendorSlashed           bool             `json:"vendor_slashed"`
	HonestReporterRewardUSD float64          `json:"honest_reporter_reward_usd"`
	VendorConflictCount     int              `json:"vendor_conflict_count"`
	ResolutionReason        string           `json:"resolution_reason"`
	SettledAt               time.Time        `json:"settled_at"`
}

// Oracle is the concurrency-safe, in-memory double-blind accountability oracle.
//
// It tracks time-bound challenge nonces, executes multi-factor settlement logic,
// prevents replay attacks, and tracks historical vendor conflict frequencies.
//
// Pre-conditions: constructed via NewOracle; zero-value Oracle requires initialization.
// Post-conditions: all exported methods acquire the internal sync.RWMutex and return deep clones.
type Oracle struct {
	mu              sync.RWMutex
	nonces          map[string]*ChallengeNonce
	outcomes        map[string]*SettlementOutcome
	vendorConflicts map[string]int
	nowFunc         func() time.Time
}

// NewOracle constructs and initializes a new concurrency-safe Oracle.
//
// Pre-conditions: none.
// Post-conditions: returns an active Oracle instance with initialized storage maps and system clock.
func NewOracle() *Oracle {
	return &Oracle{
		nonces:          make(map[string]*ChallengeNonce),
		outcomes:        make(map[string]*SettlementOutcome),
		vendorConflicts: make(map[string]int),
		nowFunc:         time.Now,
	}
}

// now returns the current time using the configured time provider.
func (o *Oracle) now() time.Time {
	if o.nowFunc != nil {
		return o.nowFunc()
	}
	return time.Now()
}

// IssueChallengeNonce allocates and stores a cryptographically secure 16-byte random hex challenge nonce.
//
// Pre-conditions: appointmentID, userDID, and vendorDID must be non-empty strings.
// Post-conditions: registers the nonce with a 60-second TTL (DefaultNonceTTL) and returns a deep copy.
func (o *Oracle) IssueChallengeNonce(appointmentID, userDID, vendorDID string) (*ChallengeNonce, error) {
	if appointmentID == "" {
		return nil, ErrEmptyAppointmentID
	}
	if userDID == "" {
		return nil, ErrEmptyUserDID
	}
	if vendorDID == "" {
		return nil, ErrEmptyVendorDID
	}

	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return nil, fmt.Errorf("oracle: failed to generate cryptographic nonce: %w", err)
	}
	nonceStr := hex.EncodeToString(raw[:])

	o.mu.Lock()
	defer o.mu.Unlock()

	issuedAt := o.now()
	cn := &ChallengeNonce{
		Nonce:         nonceStr,
		AppointmentID: appointmentID,
		UserDID:       userDID,
		VendorDID:     vendorDID,
		IssuedAt:      issuedAt,
		ExpiresAt:     issuedAt.Add(DefaultNonceTTL),
		Consumed:      false,
	}
	o.nonces[nonceStr] = cn

	cp := *cn
	return &cp, nil
}

// ExpireNonceForTest artificially sets the expiration timestamp of an existing nonce into the past.
//
// Pre-conditions: nonce must be an existing challenge nonce string; safe no-op if nonce does not exist.
// Post-conditions: ensures subsequent settlement attempts with this nonce fail with ErrNonceExpired.
func (o *Oracle) ExpireNonceForTest(nonce string) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if cn, ok := o.nonces[nonce]; ok {
		cn.ExpiresAt = o.now().Add(-1 * time.Second)
	}
}

// SettleAppointment processes presence attestations and multi-factor telemetry to settle an appointment.
//
// Pre-conditions: in.AppointmentID, in.Nonce, and in.VendorDID must be non-empty; nonce must exist and be valid.
// Post-conditions: consumes the nonce to prevent replay, determines slashing penalties and rewards,
// stores the outcome, and returns an immutable SettlementOutcome copy.
func (o *Oracle) SettleAppointment(in SettlementInput) (*SettlementOutcome, error) {
	if in.AppointmentID == "" {
		return nil, ErrEmptyAppointmentID
	}
	if in.Nonce == "" {
		return nil, ErrNonceNotFound
	}
	if in.VendorDID == "" {
		return nil, ErrEmptyVendorDID
	}

	o.mu.Lock()
	defer o.mu.Unlock()

	if _, exists := o.outcomes[in.AppointmentID]; exists {
		return nil, ErrAlreadySettled
	}

	cn, exists := o.nonces[in.Nonce]
	if !exists {
		return nil, ErrNonceNotFound
	}

	if cn.AppointmentID != in.AppointmentID {
		return nil, ErrNonceAppointmentMismatch
	}

	if cn.Consumed {
		return nil, ErrNonceConsumed
	}

	currentTime := o.now()
	if currentTime.After(cn.ExpiresAt) {
		return nil, ErrNonceExpired
	}

	// Consume nonce to guarantee replay protection.
	cn.Consumed = true

	var status SettlementStatus
	var userSlashed bool
	var vendorSlashed bool
	var rewardUSD float64
	var resolutionReason string

	if in.UserPresent && in.VendorPresent {
		// Case 1: Mutual confirmation of presence.
		status = StatusSuccess
		rewardUSD = VendorHonestReporterRewardUSD
		resolutionReason = "mutual presence attestation verified successfully"
	} else if !in.UserPresent && !in.VendorPresent {
		// Case 2: Confirmed user absence (both report user was absent).
		status = StatusSlashed
		userSlashed = true
		resolutionReason = "mutual absence confirmed: user no-show verified"
	} else {
		// Case 3: Conflicting claims (in.UserPresent != in.VendorPresent).
		status = StatusDisputed
		o.vendorConflicts[in.VendorDID]++
		conflictCount := o.vendorConflicts[in.VendorDID]

		if in.UserPresent && !in.VendorPresent {
			if in.StripePOSReceipt && in.LocalBeaconVerified {
				// Third-party hardware/POS proof confirms the user was physically present;
				// the vendor fabricated or misreported a No-Show.
				userSlashed = false
				vendorSlashed = true
				resolutionReason = fmt.Sprintf("dispute resolved: hardware beacon and POS receipt confirm user presence; vendor false report penalized (conflict count: %d)", conflictCount)
			} else if conflictCount >= VendorConflictSlashThreshold {
				// Vendor conflict count has reached or exceeded threshold.
				userSlashed = false
				vendorSlashed = true
				resolutionReason = fmt.Sprintf("dispute resolved: vendor conflict count (%d) reached slash threshold; vendor penalized", conflictCount)
			} else if !in.StripePOSReceipt && !in.LocalBeaconVerified {
				// Uncorroborated user claim against vendor No-Show report.
				userSlashed = true
				vendorSlashed = false
				resolutionReason = fmt.Sprintf("dispute resolved: user presence uncorroborated by POS receipt or beacon; user penalized (vendor conflict count: %d)", conflictCount)
			} else {
				// Partially corroborated without meeting multi-factor threshold.
				userSlashed = true
				vendorSlashed = false
				resolutionReason = fmt.Sprintf("dispute resolved: insufficient multi-factor proof to corroborate user presence; user penalized (vendor conflict count: %d)", conflictCount)
			}
		} else {
			// !in.UserPresent && in.VendorPresent: User reports absent while vendor reports present.
			// User admits absence; no-show penalized.
			userSlashed = true
			vendorSlashed = false
			resolutionReason = fmt.Sprintf("dispute resolved: user absent while vendor present; user no-show penalized (vendor conflict count: %d)", conflictCount)
		}
	}

	outcome := &SettlementOutcome{
		AppointmentID:           in.AppointmentID,
		LockID:                  in.LockID,
		Nonce:                   in.Nonce,
		Status:                  status,
		UserSlashed:             userSlashed,
		VendorSlashed:           vendorSlashed,
		HonestReporterRewardUSD: rewardUSD,
		VendorConflictCount:     o.vendorConflicts[in.VendorDID],
		ResolutionReason:        resolutionReason,
		SettledAt:               currentTime,
	}

	o.outcomes[in.AppointmentID] = outcome

	cp := *outcome
	return &cp, nil
}

// GetOutcome retrieves a copy of the settlement outcome for a given appointment ID.
//
// Pre-conditions: appointmentID must be a non-empty string.
// Post-conditions: returns a deep copy of SettlementOutcome, or ErrOutcomeNotFound if not settled.
func (o *Oracle) GetOutcome(appointmentID string) (*SettlementOutcome, error) {
	if appointmentID == "" {
		return nil, ErrEmptyAppointmentID
	}

	o.mu.RLock()
	defer o.mu.RUnlock()

	outcome, exists := o.outcomes[appointmentID]
	if !exists {
		return nil, ErrOutcomeNotFound
	}

	cp := *outcome
	return &cp, nil
}

// ListOutcomes returns a copy of all settled appointment outcomes, sorted deterministically by appointment ID.
//
// Pre-conditions: none.
// Post-conditions: returns a newly allocated slice of SettlementOutcome copies; safe from concurrent mutations.
func (o *Oracle) ListOutcomes() []*SettlementOutcome {
	o.mu.RLock()
	defer o.mu.RUnlock()

	results := make([]*SettlementOutcome, 0, len(o.outcomes))
	for _, out := range o.outcomes {
		cp := *out
		results = append(results, &cp)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].AppointmentID < results[j].AppointmentID
	})

	return results
}

// GetVendorConflictCount retrieves the cumulative number of disputed conflicts recorded for a vendor.
//
// Pre-conditions: none.
// Post-conditions: returns non-negative conflict count for vendorDID.
func (o *Oracle) GetVendorConflictCount(vendorDID string) int {
	o.mu.RLock()
	defer o.mu.RUnlock()

	return o.vendorConflicts[vendorDID]
}
