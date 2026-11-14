package ledger

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// DefaultPassportValidity defines the validity period for issued privacy-preserving trust passports.
//
// Pre-conditions: none (compile-time constant).
// Post-conditions: strictly positive duration (24 hours).
const DefaultPassportValidity = 24 * time.Hour

// Sentinel errors returned by trust passport operations.
//
// Pre-conditions: none (compile-time package-level values).
// Post-conditions: every trust passport issuance failure wraps or matches one of these errors.
var (
	// ErrPredicateFailed is returned when an agent does not satisfy requested criteria.
	ErrPredicateFailed = errors.New("ledger: agent does not satisfy requested predicate")
	// ErrInvalidSubjectDID is returned when the subject DID is empty.
	ErrInvalidSubjectDID = errors.New("ledger: subject did must not be empty")
	// ErrNilPassport is returned when a passport pointer is nil.
	ErrNilPassport = errors.New("ledger: trust passport must not be nil")
	// ErrPassportExpired is returned when a passport is verified past its expiration time.
	ErrPassportExpired = errors.New("ledger: trust passport is expired")
)

// TrustPassport represents a privacy-preserving, selective-disclosure credential
// signed by the enclave ledger, attesting to predicate fulfillment without disclosing
// individual vendor histories or booking timestamps.
//
// Pre-conditions: constructed with non-empty SubjectDID, PassportID, and MerkleRoot;
// rates are in [0.0, 1.0]; counts are non-negative.
// Post-conditions: EnclaveAttestationSig verifies against the ledger's enclave public key
// and canonical payload.
type TrustPassport struct {
	PassportID            string    `json:"passport_id"`
	SubjectDID            string    `json:"subject_did"`
	MerkleRoot            string    `json:"merkle_root"`
	MinFulfillmentRate    float64   `json:"min_fulfillment_rate"`
	MinSettledBookings    int       `json:"min_settled_bookings"`
	MinTrustTier          int       `json:"min_trust_tier"`
	CompositeScoreBucket  string    `json:"composite_score_bucket"`
	CommitmentSaltHash    string    `json:"commitment_salt_hash"`
	EnclaveAttestationSig string    `json:"enclave_attestation_sig"`
	IssuedAt              time.Time `json:"issued_at"`
	ExpiresAt             time.Time `json:"expires_at"`
}

// ScoreBucket maps a numerical composite trust score into a selective-disclosure bucket string.
//
// Pre-conditions: none (accepts any float64 score).
// Post-conditions: returns a non-empty bucket string (e.g. ">=4.5", ">=4.0", ">=3.5", ">=3.0", ">=2.5", or "<2.5").
func ScoreBucket(score float64) string {
	switch {
	case score >= 4.5:
		return ">=4.5"
	case score >= 4.0:
		return ">=4.0"
	case score >= 3.5:
		return ">=3.5"
	case score >= 3.0:
		return ">=3.0"
	case score >= 2.5:
		return ">=2.5"
	default:
		return "<2.5"
	}
}

// canonicalPassportPayload formats the attestation claim into a deterministic byte sequence
// for enclave signing and verification, omitting raw historical vendor names or appointment timestamps.
//
// Pre-conditions: p is non-nil.
// Post-conditions: returns a deterministic byte slice binding the predicate claims to MerkleRoot.
func canonicalPassportPayload(p *TrustPassport) []byte {
	return []byte(fmt.Sprintf("%s|%s|%s|%.4f|%d|%d|%s|%s|%d|%d",
		p.PassportID,
		p.SubjectDID,
		p.MerkleRoot,
		p.MinFulfillmentRate,
		p.MinSettledBookings,
		p.MinTrustTier,
		p.CompositeScoreBucket,
		p.CommitmentSaltHash,
		p.IssuedAt.Unix(),
		p.ExpiresAt.Unix(),
	))
}

// IssueTrustPassport validates that an agent meets the requested predicates and issues
// an enclave-signed selective-disclosure TrustPassport anchored to the CurrentMerkleRoot.
//
// Pre-conditions: subjectDID is non-empty; actualFulfillmentRate >= reqMinFulfillment;
// actualSettledBookings >= reqMinBookings; actualTrustTier >= reqMinTier.
// Post-conditions: on success returns an enclave-signed TrustPassport; on failure returns an error
// wrapping ErrInvalidSubjectDID or ErrPredicateFailed without mutating state.
func (l *Ledger) IssueTrustPassport(
	subjectDID string,
	actualFulfillmentRate float64,
	actualSettledBookings int,
	actualTrustTier int,
	actualScore float64,
	reqMinFulfillment float64,
	reqMinBookings int,
	reqMinTier int,
) (*TrustPassport, error) {
	if subjectDID == "" {
		return nil, ErrInvalidSubjectDID
	}
	if actualFulfillmentRate < reqMinFulfillment {
		return nil, fmt.Errorf("%w: fulfillment rate %.4f < required %.4f", ErrPredicateFailed, actualFulfillmentRate, reqMinFulfillment)
	}
	if actualSettledBookings < reqMinBookings {
		return nil, fmt.Errorf("%w: settled bookings %d < required %d", ErrPredicateFailed, actualSettledBookings, reqMinBookings)
	}
	if actualTrustTier < reqMinTier {
		return nil, fmt.Errorf("%w: trust tier %d < required %d", ErrPredicateFailed, actualTrustTier, reqMinTier)
	}

	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("ledger: failed to generate commitment salt: %w", err)
	}
	saltHash := sha256.Sum256(salt)
	commitmentSaltHash := hex.EncodeToString(saltHash[:])

	passIDBytes := make([]byte, 16)
	if _, err := rand.Read(passIDBytes); err != nil {
		return nil, fmt.Errorf("ledger: failed to generate passport id: %w", err)
	}
	passportID := "pass_" + hex.EncodeToString(passIDBytes)

	now := time.Now().UTC().Truncate(time.Second)
	passport := &TrustPassport{
		PassportID:           passportID,
		SubjectDID:           subjectDID,
		MerkleRoot:           l.CurrentMerkleRoot(),
		MinFulfillmentRate:   reqMinFulfillment,
		MinSettledBookings:   reqMinBookings,
		MinTrustTier:         reqMinTier,
		CompositeScoreBucket: ScoreBucket(actualScore),
		CommitmentSaltHash:   commitmentSaltHash,
		IssuedAt:             now,
		ExpiresAt:            now.Add(DefaultPassportValidity),
	}

	payload := canonicalPassportPayload(passport)
	l.mu.RLock()
	sig := ed25519.Sign(l.enclavePriv, payload)
	l.mu.RUnlock()

	passport.EnclaveAttestationSig = hex.EncodeToString(sig)
	return passport, nil
}

// VerifyTrustPassport verifies the validity period and enclave cryptographic signature of a TrustPassport.
//
// Pre-conditions: p may be nil or partially populated.
// Post-conditions: returns true if p is non-nil, unexpired, and its EnclaveAttestationSig is valid
// under this ledger's enclave public key; returns false on any tampering, expiration, or invalid signature.
func (l *Ledger) VerifyTrustPassport(p *TrustPassport) bool {
	if p == nil {
		return false
	}
	if !time.Now().UTC().Before(p.ExpiresAt) {
		return false
	}
	sigBytes, err := hex.DecodeString(p.EnclaveAttestationSig)
	if err != nil || len(sigBytes) != ed25519.SignatureSize {
		return false
	}

	payload := canonicalPassportPayload(p)
	l.mu.RLock()
	defer l.mu.RUnlock()
	return ed25519.Verify(l.enclavePub, payload, sigBytes)
}
