// Package identity implements the ACN sovereign identity layer: Ed25519 key
// material, `did:acn:*` decentralized identifiers, the rotating unlinkable
// Sub-Agent address generator, the 4-Tier Trust Ladder and the concurrency
// safe in-memory Registry.
//
// Design notes:
//
//   - A Root DID is a pure function of the agent Ed25519 public key
//     (`did:acn:` + hex(sha256(pubKey)[:16])), so it is self-certifying,
//     re-derivable and requires no registration service.
//   - A Sub-Agent DID uses the same hash construction with the `did:acn:sub:`
//     namespace over a freshly generated ephemeral key. Because the ephemeral
//     key is never reused and carries no correlation data, two Sub-Agents of
//     the same Root Agent are unlinkable on the wire; only the Registry keeps
//     the SubDID -> RootDID link for accountability rollup.
//   - Exposure ceilings (MaxExposureUSD) are enforced by the escrow and oracle
//     layers; this package is the single source of truth for the ladder.
package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// DID namespace constants used across the ACN protocol.
//
// Pre-conditions: none (compile-time constants).
// Post-conditions: RootDIDPrefix is the wire prefix of every sovereign Root
// DID, SubDIDPrefix is the wire prefix of every rotating Sub-Agent DID and
// didIdentifierBytes is the number of truncated SHA-256 bytes encoded in the
// identifier body (16 bytes => 32 lowercase hex characters).
const (
	// RootDIDPrefix is the mandatory prefix of a sovereign Root DID.
	//
	// Pre-conditions: none.
	// Post-conditions: every string produced by DeriveRootDID starts with it.
	RootDIDPrefix = "did:acn:"

	// SubDIDPrefix is the mandatory prefix of a rotating Sub-Agent DID.
	//
	// Pre-conditions: none.
	// Post-conditions: every string produced by Registry.SpawnSubAgent starts
	// with it; it also starts with RootDIDPrefix, so namespace checks MUST
	// test SubDIDPrefix before RootDIDPrefix.
	SubDIDPrefix = "did:acn:sub:"

	// didIdentifierBytes is the length of the truncated SHA-256 digest that
	// forms the identifier body of every ACN DID.
	didIdentifierBytes = 16
)

// Trust ladder, score and Sub-Agent generation constants.
//
// Pre-conditions: none (compile-time constants).
// Post-conditions: DefaultCompositeScore is the neutral score assigned to a
// freshly registered Root Agent, MaxCompositeScore is the inclusive upper
// bound of any composite score, and the Sub-Agent constants define the
// fallback TTL and the identifier-collision retry budget of the spawner.
const (
	// DefaultCompositeScore is the neutral reputation score given to every
	// newly registered Root Agent before any oracle settlement.
	DefaultCompositeScore = 2.50

	// MaxCompositeScore is the inclusive upper bound of CompositeScore.
	MaxCompositeScore = 5.0

	// DefaultSubAgentTTL is used when SpawnSubAgent receives a ttl <= 0.
	DefaultSubAgentTTL = 15 * time.Minute

	// subAgentDeriveAttempts is the retry budget for ephemeral identifier
	// collisions inside SpawnSubAgent.
	subAgentDeriveAttempts = 4
)

// Sentinel errors returned by the identity package. Callers may test them
// with errors.Is.
//
// Pre-conditions: none.
// Post-conditions: every error produced by this package wraps exactly one of
// these sentinels, so error classification never depends on error strings.
var (
	// ErrInvalidPublicKey indicates a malformed Ed25519 public key.
	ErrInvalidPublicKey = errors.New("identity: invalid ed25519 public key")

	// ErrInvalidPrivateKey indicates a malformed Ed25519 private key.
	ErrInvalidPrivateKey = errors.New("identity: invalid ed25519 private key")

	// ErrNilKeyPair indicates a nil *KeyPair receiver.
	ErrNilKeyPair = errors.New("identity: nil key pair")

	// ErrEmptyMessage indicates an empty message to sign or verify.
	ErrEmptyMessage = errors.New("identity: empty message")

	// ErrInvalidSignature indicates a malformed Ed25519 signature.
	ErrInvalidSignature = errors.New("identity: invalid ed25519 signature")

	// ErrRandomSource indicates a failure of the system CSPRNG.
	ErrRandomSource = errors.New("identity: random source failure")

	// ErrInvalidDID indicates a DID that is empty or malformed.
	ErrInvalidDID = errors.New("identity: invalid did")

	// ErrDuplicateRootAgent indicates a Root Agent with the same public key
	// (hence the same Root DID) is already registered.
	ErrDuplicateRootAgent = errors.New("identity: duplicate root agent")

	// ErrDuplicateSubAgent indicates an ephemeral Sub-Agent identifier
	// collision that survived the retry budget.
	ErrDuplicateSubAgent = errors.New("identity: duplicate sub-agent")

	// ErrUnknownRootAgent indicates a Root DID that is not registered.
	ErrUnknownRootAgent = errors.New("identity: unknown root agent")

	// ErrUnknownSubAgent indicates a Sub DID that was never spawned.
	ErrUnknownSubAgent = errors.New("identity: unknown sub-agent")

	// ErrInvalidPurpose indicates a blank Sub-Agent purpose.
	ErrInvalidPurpose = errors.New("identity: invalid sub-agent purpose")

	// ErrRootKeyMismatch indicates a Sub-Agent binding signature that does
	// not verify against the parent Root Agent public key.
	ErrRootKeyMismatch = errors.New("identity: sub-agent binding signature does not verify against parent root key")
)

// TrustTier is the ordinal position of a sovereign agent inside the ACN
// 4-Tier Trust Ladder. Higher tiers unlock higher unsecured exposure and
// stricter counterparties.
//
// Pre-conditions: instances must originate from CalculateTrustTier, from the
// tier constants or from a trusted deserialization source.
// Post-conditions: the zero value is Tier0Ephemeral, so an unset field fails
// closed to the least privileged tier.
type TrustTier int

// Trust ladder tiers.
//
// Pre-conditions: none (compile-time constants).
// Post-conditions: the tiers are strictly ordered 0 < 1 < 2 < 3, and each
// tier exposes its ceiling through TrustTier.MaxExposureUSD and its label
// through TrustTier.String.
const (
	// Tier0Ephemeral is the unverified entry tier, capped at 5.00 USD of
	// exposure. It is the tier of every freshly registered Root Agent.
	//
	// Pre-conditions: none; it is the fail-closed default.
	// Post-conditions: MaxExposureUSD returns 5.0 and String returns
	// "TIER_0_EPHEMERAL".
	Tier0Ephemeral TrustTier = 0

	// Tier1Economic grants 150.00 USD of exposure to agents that posted
	// proof of capital or demonstrated settlement history
	// (bondUSD >= 50.0 || settledTxCount >= 100).
	//
	// Pre-conditions: selected only by CalculateTrustTier.
	// Post-conditions: MaxExposureUSD returns 150.0 and String returns
	// "TIER_1_ECONOMIC".
	Tier1Economic TrustTier = 1

	// Tier2Reputational grants 2000.00 USD of exposure to agents with a real
	// settlement track record (bondUSD >= 50.0 && cumulativeSpendUSD >= 500.0
	// && settledTxCount >= 20).
	//
	// Pre-conditions: selected only by CalculateTrustTier.
	// Post-conditions: MaxExposureUSD returns 2000.0 and String returns
	// "TIER_2_REPUTATIONAL".
	Tier2Reputational TrustTier = 2

	// Tier3Sovereign is the unrestricted tier (1000000.00 USD ceiling)
	// reserved for proof-of-personhood verified, high reputation agents
	// (popVerified && compositeScore >= 4.5 && bondUSD >= 50.0 &&
	// cumulativeSpendUSD >= 500.0 && settledTxCount >= 20).
	//
	// Pre-conditions: selected only by CalculateTrustTier.
	// Post-conditions: MaxExposureUSD returns 1000000.0 and String returns
	// "TIER_3_SOVEREIGN".
	Tier3Sovereign TrustTier = 3
)

// String renders the canonical, wire-stable label of a TrustTier.
//
// Pre-conditions: t is any TrustTier value; values outside [0, 3] are
// tolerated.
// Post-conditions: returns "TIER_0_EPHEMERAL", "TIER_1_ECONOMIC",
// "TIER_2_REPUTATIONAL" or "TIER_3_SOVEREIGN" for the four valid tiers and
// "TIER_UNKNOWN" for any other value. The result is never empty.
func (t TrustTier) String() string {
	switch t {
	case Tier0Ephemeral:
		return "TIER_0_EPHEMERAL"
	case Tier1Economic:
		return "TIER_1_ECONOMIC"
	case Tier2Reputational:
		return "TIER_2_REPUTATIONAL"
	case Tier3Sovereign:
		return "TIER_3_SOVEREIGN"
	default:
		return "TIER_UNKNOWN"
	}
}

// MaxExposureUSD returns the maximum unsecured USD exposure the tier allows a
// counterparty to accumulate against this agent before settlement is forced.
//
// Pre-conditions: t is any TrustTier value; values outside [0, 3] are
// tolerated.
// Post-conditions: returns 5.0, 150.0, 2000.0 or 1000000.0 for tiers 0..3
// respectively and 0.0 (fail closed, no exposure) for any other value.
func (t TrustTier) MaxExposureUSD() float64 {
	switch t {
	case Tier0Ephemeral:
		return 5.0
	case Tier1Economic:
		return 150.0
	case Tier2Reputational:
		return 2000.0
	case Tier3Sovereign:
		return 1000000.0
	default:
		return 0.0
	}
}

// KeyPair holds an Ed25519 key pair used to mint and operate a sovereign
// identity. It is a value carrier only: KeyPairs are never stored inside the
// Registry, which keeps public material exclusively.
//
// Pre-conditions: PublicKey and PrivateKey are either both zero length (an
// uninitialized carrier) or both correctly sized (32 and 64 bytes) and
// matching, as produced by GenerateKeyPair.
// Post-conditions: the struct is safe to copy; the byte slices are owned by
// the holder and must not be shared with untrusted code.
type KeyPair struct {
	PublicKey  ed25519.PublicKey
	PrivateKey ed25519.PrivateKey
}

// GenerateKeyPair mints a fresh Ed25519 key pair from the system CSPRNG.
//
// Pre-conditions: none; the function reads only from crypto/rand.
// Post-conditions: on success the returned pair has a 32 byte PublicKey and a
// 64 byte PrivateKey with PrivateKey.Public() == PublicKey, and the
// probability of two calls returning the same pair is negligible. On failure
// it returns a nil pair wrapping ErrRandomSource.
func GenerateKeyPair() (*KeyPair, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRandomSource, err)
	}
	return &KeyPair{PublicKey: pub, PrivateKey: priv}, nil
}

// DeriveRootDID deterministically derives the sovereign Root DID bound to an
// Ed25519 public key.
//
// Pre-conditions: len(pub) == ed25519.PublicKeySize (32). Any other length,
// including nil, is rejected.
// Post-conditions: on success returns "did:acn:" + hex(sha256(pub)[:16]), a
// 40 character string with exactly 32 lowercase hex characters in the
// identifier body, and the result is stable across calls and processes. On
// failure returns an empty string wrapping ErrInvalidPublicKey.
func DeriveRootDID(pub ed25519.PublicKey) (string, error) {
	return deriveDID(RootDIDPrefix, pub)
}

// deriveDID is the shared hash construction behind every ACN identifier.
//
// Pre-conditions: prefix is a non-empty namespace string and len(pub) == 32.
// Post-conditions: returns prefix + hex(sha256(pub)[:16]) or an empty string
// wrapping ErrInvalidPublicKey.
func deriveDID(prefix string, pub ed25519.PublicKey) (string, error) {
	if len(pub) != ed25519.PublicKeySize {
		return "", fmt.Errorf("%w: expected %d bytes, got %d", ErrInvalidPublicKey, ed25519.PublicKeySize, len(pub))
	}
	sum := sha256.Sum256(pub)
	return prefix + hex.EncodeToString(sum[:didIdentifierBytes]), nil
}

// Sign produces the Ed25519 signature of message under the key pair private
// key.
//
// Pre-conditions: kp != nil, len(kp.PrivateKey) == ed25519.PrivateKeySize (64)
// and len(message) > 0.
// Post-conditions: on success returns a 64 byte signature that
// VerifySignature accepts for the matching public key. On failure returns a
// nil slice wrapping ErrNilKeyPair, ErrInvalidPrivateKey or ErrEmptyMessage;
// a malformed key is never used to sign.
func (kp *KeyPair) Sign(message []byte) ([]byte, error) {
	if kp == nil {
		return nil, fmt.Errorf("%w: signer is nil", ErrNilKeyPair)
	}
	if len(kp.PrivateKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("%w: expected %d bytes, got %d", ErrInvalidPrivateKey, ed25519.PrivateKeySize, len(kp.PrivateKey))
	}
	if len(message) == 0 {
		return nil, fmt.Errorf("%w: refuse to sign an empty message", ErrEmptyMessage)
	}
	return ed25519.Sign(kp.PrivateKey, message), nil
}

// VerifySignature reports whether sig is a valid Ed25519 signature of message
// under pub.
//
// Pre-conditions: pub, message and sig may have any length; malformed inputs
// are rejected rather than handed to the primitive.
// Post-conditions: returns true only when len(pub) == 32, len(sig) == 64,
// len(message) > 0 and ed25519.Verify accepts the triple. It never panics and
// never returns true for a tampered message, a foreign key or a malformed
// input.
func VerifySignature(pub ed25519.PublicKey, message, sig []byte) bool {
	if len(pub) != ed25519.PublicKeySize {
		return false
	}
	if len(sig) != ed25519.SignatureSize {
		return false
	}
	if len(message) == 0 {
		return false
	}
	return ed25519.Verify(pub, message, sig)
}

// RootAgent is the sovereign, long-lived identity of an ACN participant. It is
// the unit of accountability: every Sub-Agent infraction rolls up here, and
// the trust economics (bond, spend, settlements, score, tier) live exclusively
// on this record.
//
// Pre-conditions: instances must be obtained from Registry methods (which
// hand out deep copies) or produced by a trusted deserializer. PublicKey must
// be a 32 byte Ed25519 key when the record was produced by the Registry, and
// RootDID must equal DeriveRootDID(PublicKey).
// Post-conditions: the record is a snapshot; mutating it never mutates
// Registry state. TrustTier is always the value implied by the economic
// fields via CalculateTrustTier as of the last Registry mutation.
type RootAgent struct {
	RootDID            string    `json:"root_did"`
	DisplayName        string    `json:"display_name"`
	PublicKey          []byte    `json:"public_key"`
	PoPVerified        bool      `json:"pop_verified"`
	TEEAttested        bool      `json:"tee_attested"`
	BondUSD            float64   `json:"bond_usd"`
	CumulativeSpendUSD float64   `json:"cumulative_spend_usd"`
	SettledTxCount     int       `json:"settled_tx_count"`
	SlashCount         int       `json:"slash_count"`
	CompositeScore     float64   `json:"composite_score"`
	TrustTier          TrustTier `json:"trust_tier"`
	IsVendor           bool      `json:"is_vendor"`
	CreatedAt          time.Time `json:"created_at"`
}

// SubAgent is a rotating, unlinkable operating identity derived from a
// sovereign Root Agent. Counterparties observe only SubDID and
// EphemeralPubKey, so two Sub-Agents of the same parent cannot be correlated
// on the wire; RootSignature is the non-repudiable parent consent used by the
// accountability oracle when an infraction must be rolled up.
//
// Pre-conditions: instances must be obtained from Registry methods (deep
// copies) or from a trusted deserializer. SubDID starts with SubDIDPrefix,
// ParentRootDID starts with RootDIDPrefix, EphemeralPubKey is 32 bytes and
// RootSignature is non-empty.
// Post-conditions: the record is a point-in-time snapshot. A Sub-Agent is
// usable while time.Now() is before ExpiresAt; IsExpired is the authoritative
// predicate for that check.
type SubAgent struct {
	SubDID          string    `json:"sub_did"`
	ParentRootDID   string    `json:"parent_root_did"`
	Purpose         string    `json:"purpose"`
	EphemeralPubKey []byte    `json:"ephemeral_pub_key"`
	RootSignature   []byte    `json:"root_signature"`
	CreatedAt       time.Time `json:"created_at"`
	ExpiresAt       time.Time `json:"expires_at"`
}

// IsExpired reports whether the Sub-Agent lifetime has elapsed relative to
// now.
//
// Pre-conditions: now is a wall-clock instant chosen by the caller (pass
// time.Now().UTC() in production); s may be nil.
// Post-conditions: returns true when s is nil, when ExpiresAt is the zero
// time, or when now is at or after ExpiresAt; false otherwise. A Sub-Agent is
// therefore fail-closed once its TTL lapses.
func (s *SubAgent) IsExpired(now time.Time) bool {
	if s == nil {
		return true
	}
	if s.ExpiresAt.IsZero() {
		return true
	}
	return !now.Before(s.ExpiresAt)
}

// SubAgentBindingPayload returns the canonical byte payload a parent Root
// Agent signs when it mints a Sub-Agent, and the exact payload counterparties
// must re-derive to validate SubAgent.RootSignature.
//
// Pre-conditions: subDID is the Sub-Agent identifier, parentRootDID is the
// parent sovereign identifier and ephemeralPubKey is the ephemeral Ed25519
// public key. The fields are not validated here; the spawner validates them.
// Post-conditions: the encoding is the deterministic, unambiguous
// "acn-sub-v1|<sub_did>|<parent_root_did>|<hex(ephemeralPubKey)>" form: the
// same inputs always yield byte-identical output, and no field can be
// replayed into another one because the separator never appears in a DID or
// in a hex encoding.
func SubAgentBindingPayload(subDID, parentRootDID string, ephemeralPubKey []byte) []byte {
	var payload strings.Builder
	payload.WriteString("acn-sub-v1|")
	payload.WriteString(subDID)
	payload.WriteByte('|')
	payload.WriteString(parentRootDID)
	payload.WriteByte('|')
	payload.WriteString(hex.EncodeToString(ephemeralPubKey))
	return []byte(payload.String())
}

// CalculateTrustTier evaluates the 4-Tier Trust Ladder from the raw economic
// signals of an agent and returns the highest tier whose gate is satisfied.
//
// Pre-conditions: bondUSD and cumulativeSpendUSD are USD amounts (negative
// inputs are treated as zero), settledTxCount is a settled-transaction count
// (negative values are treated as zero), popVerified reports a validated
// proof of personhood and compositeScore is the reputation score on [0, 5].
// The function is pure: it reads no registry state and takes no lock.
// Post-conditions: returns Tier3Sovereign iff popVerified && compositeScore
// >= 4.5 && bondUSD >= 50 && cumulativeSpendUSD >= 500 && settledTxCount >= 20;
// otherwise Tier2Reputational iff bondUSD >= 50 && cumulativeSpendUSD >= 500
// && settledTxCount >= 20; otherwise Tier1Economic iff bondUSD >= 50 ||
// settledTxCount >= 100; otherwise Tier0Ephemeral. Gates are evaluated
// top-down, so a failed Tier 3 gate degrades to Tier 2 rather than to Tier 0,
// and every input combination yields a tier in [0, 3].
func CalculateTrustTier(bondUSD, cumulativeSpendUSD float64, settledTxCount int, popVerified bool, compositeScore float64) TrustTier {
	bond := nonNegative(bondUSD)
	spend := nonNegative(cumulativeSpendUSD)
	settled := settledTxCount
	if settled < 0 {
		settled = 0
	}

	capitalLocked := bond >= 50.0
	hasTrackRecord := spend >= 500.0 && settled >= 20

	switch {
	case capitalLocked && hasTrackRecord && popVerified && compositeScore >= 4.5:
		return Tier3Sovereign
	case capitalLocked && hasTrackRecord:
		return Tier2Reputational
	case capitalLocked || settled >= 100:
		return Tier1Economic
	default:
		return Tier0Ephemeral
	}
}

// nonNegative clamps a USD amount to zero when it is negative.
//
// Pre-conditions: v is any float64.
// Post-conditions: returns v when v >= 0 and 0 otherwise, guaranteeing that no
// economic field can go negative through a calculation helper.
func nonNegative(v float64) float64 {
	if v < 0 {
		return 0
	}
	return v
}
