package identity

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Registry is the concurrency-safe, in-memory authority that maps sovereign
// Root Agents to their public identities and tracks the rotating Sub-Agents
// each Root Agent has minted.
//
// The Registry is the single writer of RootAgent economics inside a node:
// every mutation (settlement deltas, slashes) goes through UpdateEconomics or
// RecordSlash, which recompute TrustTier under the write lock so the stored
// tier can never drift from the economic signals that justify it.
//
// Locking discipline: all exported methods take r.mu (read or write) exactly
// once and never call user-supplied callbacks, so the type is free of lock
// inversion and re-entrancy hazards. Every method returns deep copies, so no
// internal pointer ever escapes the critical section.
//
// Pre-conditions: constructed via NewRegistry only; a zero Registry is
// usable but provides no bootstrap guarantees.
// Post-conditions: the Registry never stores private keys; it holds public
// material and accountability metadata exclusively, and it is safe for
// concurrent use by any number of goroutines.
type Registry struct {
	mu sync.RWMutex

	// roots maps RootDID to its authoritative RootAgent record.
	roots map[string]*RootAgent
	// pubs indexes the same records by hex-encoded public key to reject
	// duplicate registrations before a DID collision is even computed.
	pubs map[string]string
	// subs maps SubDID to its authoritative SubAgent record.
	subs map[string]*SubAgent
	// subParents maps SubDID to the RootDID of its parent, giving O(1)
	// accountability rollup from a rotating identifier.
	subParents map[string]string
}

// NewRegistry allocates an empty Registry ready for use.
//
// Pre-conditions: none.
// Post-conditions: the returned Registry contains zero Root Agents and zero
// Sub-Agents, is immediately usable concurrently, and never shares mutable
// state with any other Registry instance.
func NewRegistry() *Registry {
	return &Registry{
		roots:      make(map[string]*RootAgent),
		pubs:       make(map[string]string),
		subs:       make(map[string]*SubAgent),
		subParents: make(map[string]string),
	}
}

// RegisterRootAgent registers (or idempotently returns) a sovereign Root
// Agent bound to an Ed25519 public key.
//
// When pub is empty the Registry mints a fresh key pair for the caller, which
// lets bootstrap flows pass nil and read the resulting public key off the
// returned record; when pub is supplied the caller keeps custody of the
// private key.
//
// Pre-conditions: displayName is any human-readable label (whitespace is
// trimmed); pub is either empty (nil or zero length) or exactly 32 bytes;
// popVerified, teeAttested and isVendor are the attestations asserted at
// onboarding. A non-empty, malformed pub is rejected.
// Post-conditions: on success the record is stored and a deep copy is
// returned with RootDID == DeriveRootDID(pub), CompositeScore ==
// DefaultCompositeScore (2.50), TrustTier == Tier0Ephemeral, BondUSD == 0,
// CumulativeSpendUSD == 0, SettledTxCount == 0, SlashCount == 0 and CreatedAt
// set to time.Now().UTC(). Registration is idempotent: calling it twice with
// the same public key returns the originally stored record (no field
// overwrite, no second entry). On failure it returns nil and wraps
// ErrInvalidPublicKey, ErrRandomSource or ErrDuplicateRootAgent, and no state
// is mutated.
func (r *Registry) RegisterRootAgent(displayName string, pub ed25519.PublicKey, popVerified, teeAttested, isVendor bool) (*RootAgent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(pub) == 0 {
		kp, err := GenerateKeyPair()
		if err != nil {
			return nil, err
		}
		pub = kp.PublicKey
	}
	if len(pub) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%w: expected %d bytes, got %d", ErrInvalidPublicKey, ed25519.PublicKeySize, len(pub))
	}

	pubKey := hex.EncodeToString(pub)
	rootDID, err := DeriveRootDID(pub)
	if err != nil {
		return nil, err
	}

	if existingDID, ok := r.pubs[pubKey]; ok {
		existing, exists := r.roots[existingDID]
		if !exists {
			// Index corruption cannot happen through the public API; fail
			// loudly instead of silently creating a shadow record.
			return nil, fmt.Errorf("%w: index inconsistency for %s", ErrDuplicateRootAgent, existingDID)
		}
		return cloneRootAgent(existing), nil
	}

	rec := &RootAgent{
		RootDID:        rootDID,
		DisplayName:    strings.TrimSpace(displayName),
		PublicKey:      append([]byte(nil), pub...),
		PoPVerified:    popVerified,
		TEEAttested:    teeAttested,
		CompositeScore: DefaultCompositeScore,
		TrustTier:      Tier0Ephemeral,
		IsVendor:       isVendor,
		CreatedAt:      time.Now().UTC(),
	}
	// Belt and braces: a brand new agent has no economics, so the ladder must
	// independently agree that it is Tier 0.
	rec.TrustTier = CalculateTrustTier(rec.BondUSD, rec.CumulativeSpendUSD, rec.SettledTxCount, rec.PoPVerified, rec.CompositeScore)

	r.roots[rootDID] = rec
	r.pubs[pubKey] = rootDID
	return cloneRootAgent(rec), nil
}

// SpawnSubAgent mints a fresh rotating, unlinkable Sub-Agent identity for a
// registered Root Agent and links SubDID -> RootDID for accountability rollup.
//
// The Sub-Agent always receives a brand new ephemeral Ed25519 key pair, so
// two Sub-Agents of the same parent share no observable identifier. The
// binding signature proves that the parent authorized the Sub-Agent:
//
//   - When rootPrivKey has the correct size, the canonical binding payload is
//     signed and the signature is verified against the stored parent public
//     key before the record is published; a mismatch is a hard error.
//   - When rootPrivKey is empty (the parent keeps its key off this node), a
//     deterministic SHA-256 binding digest of the same payload is stored so
//     the link is still stable and auditable, without claiming a signature.
//
// Pre-conditions: rootDID is a registered Root DID; purpose is non-empty
// after trimming; rootPrivKey is either empty or exactly 64 bytes and, when
// supplied, must correspond to rootDID; ttl <= 0 selects DefaultSubAgentTTL.
// Post-conditions: on success a SubAgent is stored and returned as a deep copy
// with SubDID == "did:acn:sub:" + hex(sha256(ephemeralPub)[:16]), a 32 byte
// EphemeralPubKey, a non-empty RootSignature, CreatedAt == time.Now().UTC()
// and ExpiresAt == CreatedAt.Add(ttl). The Sub-Agent is resolvable through
// ResolveRootAgent and ListSubAgents, it is never counted as a Root Agent, and
// no Sub-Agent ever shares an identifier or an ephemeral key with another. On
// failure it returns nil and wraps ErrInvalidDID, ErrUnknownRootAgent,
// ErrInvalidPurpose, ErrRandomSource, ErrInvalidPrivateKey,
// ErrRootKeyMismatch or ErrDuplicateSubAgent, and no state is mutated.
func (r *Registry) SpawnSubAgent(rootDID, purpose string, rootPrivKey ed25519.PrivateKey, ttl time.Duration) (*SubAgent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	normalizedDID, err := normalizeRootDID(rootDID)
	if err != nil {
		return nil, err
	}
	root, ok := r.roots[normalizedDID]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownRootAgent, normalizedDID)
	}
	trimmedPurpose := strings.TrimSpace(purpose)
	if trimmedPurpose == "" {
		return nil, fmt.Errorf("%w: purpose must not be blank", ErrInvalidPurpose)
	}
	if len(rootPrivKey) != 0 && len(rootPrivKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("%w: expected %d bytes, got %d", ErrInvalidPrivateKey, ed25519.PrivateKeySize, len(rootPrivKey))
	}
	if ttl <= 0 {
		ttl = DefaultSubAgentTTL
	}

	// A parent that hands over its private key must be the owner of the
	// registered identity; checking before signing avoids publishing a
	// Sub-Agent that could never be attributed.
	if len(rootPrivKey) == ed25519.PrivateKeySize {
		expected := hex.EncodeToString(ed25519.PrivateKey(rootPrivKey).Public().(ed25519.PublicKey))
		if expected != hex.EncodeToString(root.PublicKey) {
			return nil, fmt.Errorf("%w: %s does not own %s", ErrRootKeyMismatch, normalizedDID, expected)
		}
	}

	var (
		sub       *SubAgent
		lastErr   error
		attempted bool
	)
	for attempt := 0; attempt < subAgentDeriveAttempts; attempt++ {
		kp, err := GenerateKeyPair()
		if err != nil {
			return nil, err
		}
		subDID, err := deriveDID(SubDIDPrefix, kp.PublicKey)
		if err != nil {
			return nil, err
		}
		if _, exists := r.subs[subDID]; exists {
			attempted = true
			lastErr = fmt.Errorf("%w: %s already issued", ErrDuplicateSubAgent, subDID)
			continue
		}

		payload := SubAgentBindingPayload(subDID, normalizedDID, kp.PublicKey)
		var binding []byte
		if len(rootPrivKey) == ed25519.PrivateKeySize {
			signer := &KeyPair{PublicKey: ed25519.PublicKey(root.PublicKey), PrivateKey: rootPrivKey}
			binding, err = signer.Sign(payload)
			if err != nil {
				return nil, err
			}
			if !VerifySignature(ed25519.PublicKey(root.PublicKey), payload, binding) {
				return nil, fmt.Errorf("%w: %s", ErrRootKeyMismatch, subDID)
			}
		} else {
			// Custodial-less mode: no private key available, so publish a
			// deterministic binding digest instead of a signature.
			digest := sha256.Sum256(payload)
			binding = append([]byte(nil), digest[:]...)
		}

		now := time.Now().UTC()
		sub = &SubAgent{
			SubDID:          subDID,
			ParentRootDID:   normalizedDID,
			Purpose:         trimmedPurpose,
			EphemeralPubKey: append([]byte(nil), kp.PublicKey...),
			RootSignature:   binding,
			CreatedAt:       now,
			ExpiresAt:       now.Add(ttl),
		}
		break
	}
	if sub == nil {
		if lastErr == nil {
			lastErr = fmt.Errorf("%w: no identifier generated for %s", ErrDuplicateSubAgent, normalizedDID)
		}
		if !attempted {
			lastErr = fmt.Errorf("%w: derivation failed for %s", ErrRandomSource, normalizedDID)
		}
		return nil, lastErr
	}

	r.subs[sub.SubDID] = sub
	r.subParents[sub.SubDID] = normalizedDID
	return cloneSubAgent(sub), nil
}

// ResolveRootAgent resolves any ACN identifier to its authoritative Root
// Agent, transparently following the SubDID -> RootDID link.
//
// Pre-conditions: did is a well-formed `did:acn:<hex>` Root DID or
// `did:acn:sub:<hex>` Sub DID (case-insensitive); it must not be empty or a
// bare identifier body.
// Post-conditions: on success returns a deep copy of the underlying
// RootAgent, so the caller may mutate it without affecting Registry state. On
// failure returns nil and wraps ErrInvalidDID, ErrUnknownRootAgent or
// ErrUnknownSubAgent; a Sub DID whose parent was removed never resolves to a
// stale record.
func (r *Registry) ResolveRootAgent(did string) (*RootAgent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	root, err := r.resolveRootLocked(did)
	if err != nil {
		return nil, err
	}
	return cloneRootAgent(root), nil
}

// UpdateEconomics applies settlement deltas to a Root Agent (or, via a Sub
// DID, to its parent) and recomputes the trust tier.
//
// Deltas are additive so callers describe an event ("this settlement locked
// 20 USD of bond, spent 40 USD, settled one tx") rather than a full state
// transition, which keeps concurrent settlement reporters composable.
//
// Pre-conditions: did resolves to a registered Root Agent (Root or Sub
// namespace); bondDeltaUSD, spendDeltaUSD and settledTxDelta describe a single
// economic event; newScore >= 0 requests an explicit score rewrite (clamped to
// [0.0, MaxCompositeScore]) and newScore < 0 leaves the score untouched.
// Post-conditions: on success BondUSD, CumulativeSpendUSD and SettledTxCount
// equal the previous value plus the delta, each clamped to >= 0, CompositeScore
// is in [0.0, MaxCompositeScore], TrustTier equals CalculateTrustTier over the
// updated fields, SlashCount and CreatedAt are unchanged, and a deep copy is
// returned. On failure returns nil and wraps ErrInvalidDID,
// ErrUnknownRootAgent or ErrUnknownSubAgent, and no state is mutated.
func (r *Registry) UpdateEconomics(did string, bondDeltaUSD, spendDeltaUSD float64, settledTxDelta int, newScore float64) (*RootAgent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	root, err := r.resolveRootLocked(did)
	if err != nil {
		return nil, err
	}

	root.BondUSD = nonNegative(root.BondUSD + bondDeltaUSD)
	root.CumulativeSpendUSD = nonNegative(root.CumulativeSpendUSD + spendDeltaUSD)
	if root.SettledTxCount+settledTxDelta < 0 {
		root.SettledTxCount = 0
	} else {
		root.SettledTxCount += settledTxDelta
	}
	if newScore >= 0 {
		root.CompositeScore = clampScore(newScore)
	}
	root.TrustTier = CalculateTrustTier(root.BondUSD, root.CumulativeSpendUSD, root.SettledTxCount, root.PoPVerified, root.CompositeScore)
	return cloneRootAgent(root), nil
}

// RecordSlash records an infraction against an identity and rolls it up to the
// sovereign Root Agent.
//
// This is the accountability choke point: a Sub DID is accepted so that a
// misbehaving rotating Sub-Agent damages the parent economics directly, which
// is what makes unlinkable Sub-Agents economically safe to issue.
//
// Pre-conditions: did resolves to a registered Root Agent (Root or Sub
// namespace); bondPenaltyUSD >= 0 is the bond amount to deduct (a negative
// penalty is treated as zero, so a slash can never mint bond); newScore is the
// adjudicated reputation score and is clamped to [0.0, MaxCompositeScore].
// Post-conditions: on success SlashCount is incremented by exactly one,
// BondUSD equals max(0, previous - bondPenaltyUSD), CompositeScore equals the
// clamped newScore, TrustTier equals CalculateTrustTier over the updated
// fields (so a slash can demote Tier 3 to Tier 0 in one step), spend and
// settled-transaction history are preserved, and a deep copy is returned. On
// failure returns nil and wraps ErrInvalidDID, ErrUnknownRootAgent or
// ErrUnknownSubAgent, and no state is mutated.
func (r *Registry) RecordSlash(did string, bondPenaltyUSD float64, newScore float64) (*RootAgent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	root, err := r.resolveRootLocked(did)
	if err != nil {
		return nil, err
	}

	penalty := nonNegative(bondPenaltyUSD)
	root.SlashCount++
	root.BondUSD = nonNegative(root.BondUSD - penalty)
	root.CompositeScore = clampScore(newScore)
	root.TrustTier = CalculateTrustTier(root.BondUSD, root.CumulativeSpendUSD, root.SettledTxCount, root.PoPVerified, root.CompositeScore)
	return cloneRootAgent(root), nil
}

// ListRootAgents returns a deterministic snapshot of every registered Root
// Agent.
//
// Pre-conditions: none.
// Post-conditions: returns a freshly allocated slice ordered ascending by
// RootDID (so two calls on unchanged state produce identical output, which
// makes Merkle batching and API responses reproducible), each element a deep
// copy. It never returns nil for an empty Registry and never aliases internal
// records.
func (r *Registry) ListRootAgents() []*RootAgent {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]*RootAgent, 0, len(r.roots))
	for _, rec := range r.roots {
		out = append(out, cloneRootAgent(rec))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RootDID < out[j].RootDID })
	return out
}

// ListSubAgents returns a deterministic snapshot of the Sub-Agents minted by a
// Root Agent, or of the whole Sub-Agent population.
//
// Pre-conditions: rootDID is a well-formed Root DID, or the empty string to
// request every Sub-Agent in the Registry. A non-empty, malformed rootDID is
// rejected rather than treated as "all".
// Post-conditions: returns a freshly allocated slice ordered ascending by
// SubDID, each element a deep copy; only Sub-Agents whose parent still
// resolves are included, and it never returns nil for an empty result.
func (r *Registry) ListSubAgents(rootDID string) []*SubAgent {
	r.mu.RLock()
	defer r.mu.RUnlock()

	filter := ""
	if rootDID != "" {
		if normalized, err := normalizeRootDID(rootDID); err == nil {
			filter = normalized
		} else {
			return []*SubAgent{}
		}
	}

	out := make([]*SubAgent, 0, len(r.subs))
	for subDID, rec := range r.subs {
		parent, ok := r.subParents[subDID]
		if !ok {
			continue
		}
		if filter != "" {
			if parent != filter {
				continue
			}
		}
		if _, alive := r.roots[parent]; !alive {
			continue
		}
		out = append(out, cloneSubAgent(rec))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SubDID < out[j].SubDID })
	return out
}

// resolveRootLocked maps any accepted identifier to the authoritative
// RootAgent record.
//
// Pre-conditions: r.mu must be held (read or write) by the caller for the
// whole call. did may be a Root DID or a Sub DID.
// Post-conditions: returns the live internal pointer (callers MUST clone it
// before returning it externally) or an error wrapping ErrInvalidDID,
// ErrUnknownRootAgent or ErrUnknownSubAgent. Sub DID resolution is attempted
// before Root DID resolution because every Sub DID also matches the Root
// namespace prefix.
func (r *Registry) resolveRootLocked(did string) (*RootAgent, error) {
	normalized, err := normalizeDID(did)
	if err != nil {
		return nil, err
	}

	if strings.HasPrefix(normalized, SubDIDPrefix) {
		parent, ok := r.subParents[normalized]
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrUnknownSubAgent, normalized)
		}
		root, ok := r.roots[parent]
		if !ok {
			return nil, fmt.Errorf("%w: %s has no live parent", ErrUnknownSubAgent, normalized)
		}
		return root, nil
	}

	root, ok := r.roots[normalized]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownRootAgent, normalized)
	}
	return root, nil
}

// normalizeDID canonicalizes an arbitrary identifier into its lowercase wire
// form.
//
// Pre-conditions: did may carry surrounding whitespace or uppercase hex; a
// bare identifier body (no prefix) is accepted and promoted to the Root
// namespace.
// Post-conditions: returns the canonical identifier, or an empty string plus
// an error wrapping ErrInvalidDID when did is empty or lacks both namespaces.
func normalizeDID(did string) (string, error) {
	trimmed := strings.TrimSpace(did)
	if trimmed == "" {
		return "", fmt.Errorf("%w: did must not be empty", ErrInvalidDID)
	}
	lowered := strings.ToLower(trimmed)
	switch {
	case strings.HasPrefix(lowered, SubDIDPrefix):
		return normalizeDIDSuffix(lowered, SubDIDPrefix)
	case strings.HasPrefix(lowered, RootDIDPrefix):
		return normalizeDIDSuffix(lowered, RootDIDPrefix)
	default:
		return normalizeDIDSuffix(RootDIDPrefix+lowered, RootDIDPrefix)
	}
}

// normalizeRootDID canonicalizes an identifier that must live in the Root
// namespace.
//
// Pre-conditions: did may carry whitespace, uppercase hex or a bare
// identifier body; a Sub DID is not accepted.
// Post-conditions: returns the canonical Root DID or an error wrapping
// ErrInvalidDID when did is empty or belongs to the Sub namespace.
func normalizeRootDID(did string) (string, error) {
	normalized, err := normalizeDID(did)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(normalized, SubDIDPrefix) {
		return "", fmt.Errorf("%w: %s is a sub-agent did, expected a root did", ErrInvalidDID, normalized)
	}
	return normalized, nil
}

// normalizeDIDSuffix validates the identifier body of a prefixed DID.
//
// Pre-conditions: prefixed starts with prefix and prefix is one of the
// namespace constants.
// Post-conditions: returns prefixed unchanged when its body is non-empty and
// decodes as hex; otherwise returns an empty string plus ErrInvalidDID. The
// check rejects typos such as "did:acn:zzzz" instead of storing an
// unparseable identifier.
func normalizeDIDSuffix(prefixed, prefix string) (string, error) {
	body := strings.TrimPrefix(prefixed, prefix)
	if body == "" {
		return "", fmt.Errorf("%w: %q has an empty identifier body", ErrInvalidDID, prefixed)
	}
	if _, err := hex.DecodeString(body); err != nil {
		return "", fmt.Errorf("%w: %q has a non-hex identifier body: %v", ErrInvalidDID, prefixed, err)
	}
	return prefixed, nil
}

// clampScore constrains a composite score to the legal reputation range.
//
// Pre-conditions: score is any float64.
// Post-conditions: returns 0.0 for negative or NaN input, MaxCompositeScore
// for anything above it, and score otherwise, so CompositeScore always stays
// inside [0.0, 5.0].
func clampScore(score float64) float64 {
	if !(score > 0) { // covers negative values and NaN.
		return 0
	}
	if score > MaxCompositeScore {
		return MaxCompositeScore
	}
	return score
}

// cloneRootAgent deep copies a RootAgent record.
//
// Pre-conditions: src is non-nil.
// Post-conditions: the returned record shares no byte slice with src, so
// callers may freely mutate or serialize it without racing the Registry.
func cloneRootAgent(src *RootAgent) *RootAgent {
	if src == nil {
		return nil
	}
	dst := *src
	dst.PublicKey = append([]byte(nil), src.PublicKey...)
	return &dst
}

// cloneSubAgent deep copies a SubAgent record.
//
// Pre-conditions: src is non-nil.
// Post-conditions: the returned record shares no byte slice with src, so
// counterparty-facing code may mutate or serialize it without racing the
// Registry.
func cloneSubAgent(src *SubAgent) *SubAgent {
	if src == nil {
		return nil
	}
	dst := *src
	dst.EphemeralPubKey = append([]byte(nil), src.EphemeralPubKey...)
	dst.RootSignature = append([]byte(nil), src.RootSignature...)
	return &dst
}
