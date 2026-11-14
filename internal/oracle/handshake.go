package oracle

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// DefaultNonceTTL defines the validity duration for newly issued challenge nonces.
//
// Pre-conditions: none.
// Post-conditions: defines a 60-second validity window for mutual attestation.
const DefaultNonceTTL = 60 * time.Second

// VendorHonestReporterRewardUSD is the micro-reward credited to a vendor upon successful mutual attestation.
//
// Pre-conditions: none.
// Post-conditions: defines the standard 0.25 USD incentive for honest attestation.
const VendorHonestReporterRewardUSD = 0.25

// VendorConflictSlashThreshold is the threshold of disputed conflicts at or above which an uncorroborated vendor is penalized.
//
// Pre-conditions: none.
// Post-conditions: defines 3 as the maximum tolerated unresolved conflicts before severe vendor penalties.
const VendorConflictSlashThreshold = 3

// ChallengeNonce represents a single-use, time-bound cryptographic challenge
// issued by the Oracle to verify physical proximity and check-in between an
// appointment user and vendor.
//
// Pre-conditions: Nonce is a non-empty 16-byte random hex string; IssuedAt precedes ExpiresAt.
// Post-conditions: tracks single-use state via Consumed to prevent replay attacks across sessions.
type ChallengeNonce struct {
	Nonce         string    `json:"nonce"`
	AppointmentID string    `json:"appointment_id"`
	UserDID       string    `json:"user_did"`
	VendorDID     string    `json:"vendor_did"`
	IssuedAt      time.Time `json:"issued_at"`
	ExpiresAt     time.Time `json:"expires_at"`
	Consumed      bool      `json:"consumed"`
}

// SignedPresenceToken encapsulates a cryptographically signed attestation of
// physical presence (or absence) by an appointment participant over a challenge nonce.
//
// Pre-conditions: AppointmentID, Nonce, SignerDID, and Signature must be non-empty strings.
// Post-conditions: provides HMAC-verifiable evidence of presence or absence for settlement.
type SignedPresenceToken struct {
	AppointmentID string    `json:"appointment_id"`
	Nonce         string    `json:"nonce"`
	SignerDID     string    `json:"signer_did"`
	Present       bool      `json:"present"`
	Signature     string    `json:"signature"`
	Timestamp     time.Time `json:"timestamp"`
}

// ComputePresenceSignature calculates a deterministic HMAC-SHA256 signature
// over the presence attestation payload: "appointmentID|nonce|signerDID|present".
//
// Pre-conditions: secret must be a non-empty HMAC key string.
// Post-conditions: returns lower-case hex-encoded HMAC-SHA256 digest of the canonical attestation string.
func ComputePresenceSignature(secret string, appointmentID, nonce, signerDID string, present bool) string {
	mac := hmac.New(sha256.New, []byte(secret))
	payload := fmt.Sprintf("%s|%s|%s|%t", appointmentID, nonce, signerDID, present)
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyPresenceSignature validates an attestation signature in constant time
// against the expected HMAC-SHA256 digest.
//
// Pre-conditions: secret must be non-empty; token.Signature must be a hex-encoded digest.
// Post-conditions: returns true if and only if token.Signature matches expected HMAC-SHA256 digest in constant time.
func VerifyPresenceSignature(secret string, token SignedPresenceToken) bool {
	if secret == "" || token.Signature == "" {
		return false
	}
	expectedHex := ComputePresenceSignature(secret, token.AppointmentID, token.Nonce, token.SignerDID, token.Present)
	expectedBytes, err1 := hex.DecodeString(expectedHex)
	sigBytes, err2 := hex.DecodeString(token.Signature)
	if err1 != nil || err2 != nil {
		return false
	}
	return hmac.Equal(sigBytes, expectedBytes)
}
