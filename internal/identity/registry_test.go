package identity

import (
	"bytes"
	"crypto/ed25519"
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestKeyPairAndSignVerify exercises Ed25519 key generation, message signing
// and signature verification against valid and tampered payloads.
//
// Pre-conditions: system CSPRNG is functional.
// Post-conditions: all generated keys are valid 32/64 bytes, signing works,
// signature verification accepts legitimate signatures and rejects tampered ones.
func TestKeyPairAndSignVerify(t *testing.T) {
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair failed: %v", err)
	}
	if len(kp.PublicKey) != ed25519.PublicKeySize {
		t.Fatalf("expected public key size %d, got %d", ed25519.PublicKeySize, len(kp.PublicKey))
	}
	if len(kp.PrivateKey) != ed25519.PrivateKeySize {
		t.Fatalf("expected private key size %d, got %d", ed25519.PrivateKeySize, len(kp.PrivateKey))
	}

	kp2, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair (second) failed: %v", err)
	}
	if bytes.Equal(kp.PublicKey, kp2.PublicKey) {
		t.Fatalf("two independently generated keypairs share public keys")
	}

	msg := []byte("agent-settlement-payload-v1")
	sig, err := kp.Sign(msg)
	if err != nil {
		t.Fatalf("Sign failed: %v", err)
	}
	if len(sig) != ed25519.SignatureSize {
		t.Fatalf("expected signature size %d, got %d", ed25519.SignatureSize, len(sig))
	}

	if !VerifySignature(kp.PublicKey, msg, sig) {
		t.Fatalf("VerifySignature rejected valid signature")
	}

	// Tampered message
	tamperedMsg := []byte("agent-settlement-payload-v2")
	if VerifySignature(kp.PublicKey, tamperedMsg, sig) {
		t.Fatalf("VerifySignature accepted signature for tampered message")
	}

	// Tampered signature
	corruptSig := append([]byte(nil), sig...)
	corruptSig[0] ^= 0xff
	if VerifySignature(kp.PublicKey, msg, corruptSig) {
		t.Fatalf("VerifySignature accepted corrupted signature")
	}

	// Wrong public key
	if VerifySignature(kp2.PublicKey, msg, sig) {
		t.Fatalf("VerifySignature accepted signature under wrong public key")
	}

	// Edge cases: empty message, malformed pub/sig
	if VerifySignature(kp.PublicKey, nil, sig) {
		t.Fatalf("VerifySignature accepted empty message")
	}
	if VerifySignature([]byte{1, 2, 3}, msg, sig) {
		t.Fatalf("VerifySignature accepted short public key")
	}
	if VerifySignature(kp.PublicKey, msg, []byte{1, 2, 3}) {
		t.Fatalf("VerifySignature accepted short signature")
	}

	// Sign edge cases
	var nilKP *KeyPair
	if _, err := nilKP.Sign(msg); err == nil {
		t.Fatalf("nil KeyPair.Sign succeeded unexpectedly")
	}
	badPrivKP := &KeyPair{PublicKey: kp.PublicKey, PrivateKey: []byte{1, 2, 3}}
	if _, err := badPrivKP.Sign(msg); err == nil {
		t.Fatalf("bad private key KeyPair.Sign succeeded unexpectedly")
	}
	if _, err := kp.Sign(nil); err == nil {
		t.Fatalf("Sign with empty message succeeded unexpectedly")
	}
}

// TestDeriveRootDID validates deterministic derivation of sovereign Root DIDs
// and error handling on malformed keys.
//
// Pre-conditions: valid and invalid Ed25519 public keys.
// Post-conditions: valid keys produce deterministic "did:acn:" prefixed 40-char
// strings; invalid keys return ErrInvalidPublicKey.
func TestDeriveRootDID(t *testing.T) {
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair failed: %v", err)
	}

	did1, err := DeriveRootDID(kp.PublicKey)
	if err != nil {
		t.Fatalf("DeriveRootDID failed: %v", err)
	}
	did2, err := DeriveRootDID(kp.PublicKey)
	if err != nil {
		t.Fatalf("DeriveRootDID failed on second run: %v", err)
	}
	if did1 != did2 {
		t.Fatalf("DeriveRootDID is non-deterministic: %s != %s", did1, did2)
	}

	if len(did1) != len(RootDIDPrefix)+32 {
		t.Fatalf("expected DID length %d, got %d (%s)", len(RootDIDPrefix)+32, len(did1), did1)
	}
	if did1[:len(RootDIDPrefix)] != RootDIDPrefix {
		t.Fatalf("expected DID prefix %s, got %s", RootDIDPrefix, did1)
	}

	// Invalid pubkey lengths
	if _, err := DeriveRootDID(nil); err == nil {
		t.Fatalf("DeriveRootDID accepted nil public key")
	}
	if _, err := DeriveRootDID([]byte("short-key")); err == nil {
		t.Fatalf("DeriveRootDID accepted short public key")
	}
}

// TestTrustTierProperties exercises the String and MaxExposureUSD methods of
// the TrustTier ladder enumeration.
//
// Pre-conditions: valid TrustTier constants and out-of-range sentinel values.
// Post-conditions: string labels and max USD exposures match the formal specification.
func TestTrustTierProperties(t *testing.T) {
	cases := []struct {
		tier         TrustTier
		wantStr      string
		wantExposure float64
	}{
		{Tier0Ephemeral, "TIER_0_EPHEMERAL", 5.0},
		{Tier1Economic, "TIER_1_ECONOMIC", 150.0},
		{Tier2Reputational, "TIER_2_REPUTATIONAL", 2000.0},
		{Tier3Sovereign, "TIER_3_SOVEREIGN", 1000000.0},
		{TrustTier(99), "TIER_UNKNOWN", 0.0},
		{TrustTier(-1), "TIER_UNKNOWN", 0.0},
	}

	for _, c := range cases {
		if got := c.tier.String(); got != c.wantStr {
			t.Errorf("tier %d: got string %q, want %q", c.tier, got, c.wantStr)
		}
		if got := c.tier.MaxExposureUSD(); got != c.wantExposure {
			t.Errorf("tier %d: got exposure %v, want %v", c.tier, got, c.wantExposure)
		}
	}
}

// TestCalculateTrustTier validates the 4-Tier ladder calculation across all
// ladder thresholds and degradation gates.
//
// Pre-conditions: varying bond, spend, settled transaction count, PoP and composite score.
// Post-conditions: calculated tier matches the expected rung without drift.
func TestCalculateTrustTier(t *testing.T) {
	tests := []struct {
		name               string
		bondUSD            float64
		cumulativeSpendUSD float64
		settledTxCount     int
		popVerified        bool
		compositeScore     float64
		wantTier           TrustTier
	}{
		{
			name:               "tier 0: brand new agent",
			bondUSD:            0,
			cumulativeSpendUSD: 0,
			settledTxCount:     0,
			popVerified:        false,
			compositeScore:     2.5,
			wantTier:           Tier0Ephemeral,
		},
		{
			name:               "tier 1: via bond alone",
			bondUSD:            50.0,
			cumulativeSpendUSD: 0,
			settledTxCount:     0,
			popVerified:        false,
			compositeScore:     2.5,
			wantTier:           Tier1Economic,
		},
		{
			name:               "tier 1: via settled tx alone",
			bondUSD:            0,
			cumulativeSpendUSD: 0,
			settledTxCount:     100,
			popVerified:        false,
			compositeScore:     2.5,
			wantTier:           Tier1Economic,
		},
		{
			name:               "tier 1: via settled tx with spend but no bond",
			bondUSD:            10.0,
			cumulativeSpendUSD: 600.0,
			settledTxCount:     105,
			popVerified:        false,
			compositeScore:     3.0,
			wantTier:           Tier1Economic,
		},
		{
			name:               "tier 2: reputational requirements met",
			bondUSD:            50.0,
			cumulativeSpendUSD: 500.0,
			settledTxCount:     20,
			popVerified:        false,
			compositeScore:     3.0,
			wantTier:           Tier2Reputational,
		},
		{
			name:               "tier 2: reputational with high score but no pop",
			bondUSD:            100.0,
			cumulativeSpendUSD: 1000.0,
			settledTxCount:     50,
			popVerified:        false,
			compositeScore:     4.9,
			wantTier:           Tier2Reputational,
		},
		{
			name:               "tier 2: reputational with pop but score below 4.5",
			bondUSD:            100.0,
			cumulativeSpendUSD: 1000.0,
			settledTxCount:     50,
			popVerified:        true,
			compositeScore:     4.49,
			wantTier:           Tier2Reputational,
		},
		{
			name:               "tier 3: sovereign requirements fully satisfied",
			bondUSD:            50.0,
			cumulativeSpendUSD: 500.0,
			settledTxCount:     20,
			popVerified:        true,
			compositeScore:     4.5,
			wantTier:           Tier3Sovereign,
		},
		{
			name:               "tier 3: sovereign with margin",
			bondUSD:            1000.0,
			cumulativeSpendUSD: 50000.0,
			settledTxCount:     250,
			popVerified:        true,
			compositeScore:     4.95,
			wantTier:           Tier3Sovereign,
		},
		{
			name:               "edge: negative values treated as zero",
			bondUSD:            -50.0,
			cumulativeSpendUSD: -500.0,
			settledTxCount:     -10,
			popVerified:        true,
			compositeScore:     5.0,
			wantTier:           Tier0Ephemeral,
		},
		{
			name:               "edge: high spend but bond insufficient and tx < 100",
			bondUSD:            49.99,
			cumulativeSpendUSD: 10000.0,
			settledTxCount:     99,
			popVerified:        true,
			compositeScore:     5.0,
			wantTier:           Tier0Ephemeral,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateTrustTier(tt.bondUSD, tt.cumulativeSpendUSD, tt.settledTxCount, tt.popVerified, tt.compositeScore)
			if got != tt.wantTier {
				t.Fatalf("CalculateTrustTier() = %v (%s), want %v (%s)", got, got, tt.wantTier, tt.wantTier)
			}
		})
	}
}

// TestRegisterRootAgent validates creation, idempotency, default economics and
// key generation for Root Agents.
//
// Pre-conditions: fresh Registry instance.
// Post-conditions: registering with or without explicit pubkey produces valid
// RootAgent records; duplicate registrations return identical records without overwriting.
func TestRegisterRootAgent(t *testing.T) {
	reg := NewRegistry()

	// 1. Explicit key
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair failed: %v", err)
	}
	root1, err := reg.RegisterRootAgent("Agent Alpha", kp.PublicKey, true, false, true)
	if err != nil {
		t.Fatalf("RegisterRootAgent failed: %v", err)
	}
	if root1.DisplayName != "Agent Alpha" {
		t.Errorf("expected DisplayName %q, got %q", "Agent Alpha", root1.DisplayName)
	}
	if !root1.PoPVerified || root1.TEEAttested || !root1.IsVendor {
		t.Errorf("attestation flags mismatch: %+v", root1)
	}
	if root1.CompositeScore != DefaultCompositeScore {
		t.Errorf("expected composite score %v, got %v", DefaultCompositeScore, root1.CompositeScore)
	}
	if root1.TrustTier != Tier0Ephemeral {
		t.Errorf("expected initial tier Tier0Ephemeral, got %v", root1.TrustTier)
	}
	if root1.CreatedAt.IsZero() {
		t.Errorf("CreatedAt was zero")
	}

	// Idempotent re-registration
	root1Dup, err := reg.RegisterRootAgent("Agent Alpha Altered Name", kp.PublicKey, false, true, false)
	if err != nil {
		t.Fatalf("re-registration failed: %v", err)
	}
	if root1Dup.RootDID != root1.RootDID {
		t.Errorf("re-registration changed RootDID: %s vs %s", root1Dup.RootDID, root1.RootDID)
	}
	if root1Dup.DisplayName != "Agent Alpha" {
		t.Errorf("re-registration mutated existing record: %q", root1Dup.DisplayName)
	}

	// 2. Automatic key generation (pub == nil)
	rootAuto, err := reg.RegisterRootAgent("Auto Key Agent", nil, false, true, false)
	if err != nil {
		t.Fatalf("RegisterRootAgent with nil pub failed: %v", err)
	}
	if len(rootAuto.PublicKey) != ed25519.PublicKeySize {
		t.Fatalf("expected auto public key size %d, got %d", ed25519.PublicKeySize, len(rootAuto.PublicKey))
	}
	if rootAuto.RootDID == "" {
		t.Fatalf("auto-generated agent has empty RootDID")
	}

	// 3. Invalid public key length
	if _, err := reg.RegisterRootAgent("Bad Key", []byte{1, 2, 3}, false, false, false); err == nil {
		t.Fatalf("RegisterRootAgent accepted invalid public key length")
	}
}

// TestSpawnSubAgentAndUnlinkability validates sub-agent derivation, parent
// signature binding, unlinkability across sibling sub-agents and expiration.
//
// Pre-conditions: registered Root Agent in Registry.
// Post-conditions: spawned sub-agents have distinct ephemeral public keys and
// DIDs, valid parent binding signatures and proper TTL expiration handling.
func TestSpawnSubAgentAndUnlinkability(t *testing.T) {
	reg := NewRegistry()
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair failed: %v", err)
	}

	root, err := reg.RegisterRootAgent("Parent Agent", kp.PublicKey, true, false, false)
	if err != nil {
		t.Fatalf("RegisterRootAgent failed: %v", err)
	}

	// Spawn Sub-Agent 1 with private key signature
	sub1, err := reg.SpawnSubAgent(root.RootDID, "execution-worker-1", kp.PrivateKey, 30*time.Minute)
	if err != nil {
		t.Fatalf("SpawnSubAgent failed: %v", err)
	}
	if sub1.ParentRootDID != root.RootDID {
		t.Errorf("expected parent %s, got %s", root.RootDID, sub1.ParentRootDID)
	}
	if sub1.Purpose != "execution-worker-1" {
		t.Errorf("expected purpose execution-worker-1, got %s", sub1.Purpose)
	}
	if len(sub1.EphemeralPubKey) != ed25519.PublicKeySize {
		t.Fatalf("expected ephemeral public key size %d, got %d", ed25519.PublicKeySize, len(sub1.EphemeralPubKey))
	}
	if len(sub1.RootSignature) != ed25519.SignatureSize {
		t.Fatalf("expected Ed25519 signature size %d, got %d", ed25519.SignatureSize, len(sub1.RootSignature))
	}

	// Verify binding signature against root public key
	payload1 := SubAgentBindingPayload(sub1.SubDID, root.RootDID, sub1.EphemeralPubKey)
	if !VerifySignature(kp.PublicKey, payload1, sub1.RootSignature) {
		t.Fatalf("sub1 RootSignature failed verification against parent public key")
	}

	// Spawn Sub-Agent 2 under same parent (unlinkability check)
	sub2, err := reg.SpawnSubAgent(root.RootDID, "execution-worker-2", kp.PrivateKey, 0)
	if err != nil {
		t.Fatalf("SpawnSubAgent 2 failed: %v", err)
	}
	if sub1.SubDID == sub2.SubDID {
		t.Fatalf("colliding SubDID between sibling sub-agents: %s", sub1.SubDID)
	}
	if bytes.Equal(sub1.EphemeralPubKey, sub2.EphemeralPubKey) {
		t.Fatalf("sibling sub-agents shared ephemeral public key")
	}

	// Custodial-less mode (rootPrivKey is nil/empty)
	subNoPriv, err := reg.SpawnSubAgent(root.RootDID, "custodial-less-worker", nil, 10*time.Minute)
	if err != nil {
		t.Fatalf("SpawnSubAgent custodial-less failed: %v", err)
	}
	if len(subNoPriv.RootSignature) != 32 {
		t.Fatalf("expected 32-byte sha256 digest signature in custodial-less mode, got %d", len(subNoPriv.RootSignature))
	}

	// Expiration checks
	if sub1.IsExpired(time.Now().UTC()) {
		t.Fatalf("sub1 reported expired immediately upon spawn")
	}
	if !sub1.IsExpired(time.Now().UTC().Add(31 * time.Minute)) {
		t.Fatalf("sub1 reported not expired past its TTL")
	}
	var nilSub *SubAgent
	if !nilSub.IsExpired(time.Now().UTC()) {
		t.Fatalf("nil SubAgent reported not expired")
	}

	// Spawn error conditions
	if _, err := reg.SpawnSubAgent(root.RootDID, "", kp.PrivateKey, time.Minute); err == nil {
		t.Fatalf("SpawnSubAgent accepted blank purpose")
	}
	if _, err := reg.SpawnSubAgent("did:acn:unknown0000000000000000000000000", "test", kp.PrivateKey, time.Minute); err == nil {
		t.Fatalf("SpawnSubAgent accepted unknown root DID")
	}
	foreignKP, _ := GenerateKeyPair()
	if _, err := reg.SpawnSubAgent(root.RootDID, "test", foreignKP.PrivateKey, time.Minute); err == nil {
		t.Fatalf("SpawnSubAgent accepted mismatched private key")
	}
	if _, err := reg.SpawnSubAgent(root.RootDID, "test", []byte{1, 2, 3}, time.Minute); err == nil {
		t.Fatalf("SpawnSubAgent accepted malformed private key length")
	}
}

// TestResolveRootAgent validates resolution of Root DIDs and Sub DIDs to the
// authoritative root agent snapshot.
//
// Pre-conditions: registry with registered root and spawned sub-agents.
// Post-conditions: resolving by RootDID or SubDID yields the identical parent
// root agent; snapshot mutations do not corrupt registry state.
func TestResolveRootAgent(t *testing.T) {
	reg := NewRegistry()
	kp, _ := GenerateKeyPair()
	root, err := reg.RegisterRootAgent("Primary Agent", kp.PublicKey, true, true, false)
	if err != nil {
		t.Fatalf("RegisterRootAgent failed: %v", err)
	}
	sub, err := reg.SpawnSubAgent(root.RootDID, "sub-worker", kp.PrivateKey, time.Hour)
	if err != nil {
		t.Fatalf("SpawnSubAgent failed: %v", err)
	}

	// Resolve by Root DID
	res1, err := reg.ResolveRootAgent(root.RootDID)
	if err != nil {
		t.Fatalf("ResolveRootAgent(root.RootDID) failed: %v", err)
	}
	if res1.RootDID != root.RootDID {
		t.Errorf("resolved root DID mismatch: %s vs %s", res1.RootDID, root.RootDID)
	}

	// Resolve by Sub DID
	resSub, err := reg.ResolveRootAgent(sub.SubDID)
	if err != nil {
		t.Fatalf("ResolveRootAgent(sub.SubDID) failed: %v", err)
	}
	if resSub.RootDID != root.RootDID {
		t.Errorf("resolving sub DID did not return parent root DID: %s vs %s", resSub.RootDID, root.RootDID)
	}

	// Mutation safety: mutating returned RootAgent does not affect registry
	res1.DisplayName = "Hacked Name"
	res2, _ := reg.ResolveRootAgent(root.RootDID)
	if res2.DisplayName == "Hacked Name" {
		t.Fatalf("internal registry state mutated via returned pointer")
	}

	// Unknown identifiers
	if _, err := reg.ResolveRootAgent("did:acn:99999999999999999999999999999999"); err == nil {
		t.Fatalf("ResolveRootAgent accepted unknown root DID")
	}
	if _, err := reg.ResolveRootAgent("did:acn:sub:99999999999999999999999999999999"); err == nil {
		t.Fatalf("ResolveRootAgent accepted unknown sub DID")
	}
	if _, err := reg.ResolveRootAgent(""); err == nil {
		t.Fatalf("ResolveRootAgent accepted empty DID")
	}
}

// TestEconomicsAndSlashRollup verifies economic updates and slash rollups from
// rotating Sub-Agents directly to parent Root Agents, including ladder demotion.
//
// Pre-conditions: registered root agent with spawned sub-agent.
// Post-conditions: deltas accumulate correctly; slashes on sub-agents deduct parent
// bond, increment slash count and demote trust tier immediately.
func TestEconomicsAndSlashRollup(t *testing.T) {
	reg := NewRegistry()
	kp, _ := GenerateKeyPair()
	root, err := reg.RegisterRootAgent("Econ Test Agent", kp.PublicKey, true, false, false)
	if err != nil {
		t.Fatalf("RegisterRootAgent failed: %v", err)
	}
	sub, err := reg.SpawnSubAgent(root.RootDID, "trading-sub", kp.PrivateKey, time.Hour)
	if err != nil {
		t.Fatalf("SpawnSubAgent failed: %v", err)
	}

	// Initial tier is 0
	if root.TrustTier != Tier0Ephemeral {
		t.Fatalf("initial tier must be Tier0Ephemeral, got %v", root.TrustTier)
	}

	// 1. Update economics via SubDID to reach Tier 1
	updated, err := reg.UpdateEconomics(sub.SubDID, 50.0, 10.0, 5, -1)
	if err != nil {
		t.Fatalf("UpdateEconomics via sub failed: %v", err)
	}
	if updated.BondUSD != 50.0 || updated.CumulativeSpendUSD != 10.0 || updated.SettledTxCount != 5 {
		t.Fatalf("economic state mismatch: %+v", updated)
	}
	if updated.TrustTier != Tier1Economic {
		t.Fatalf("expected promotion to Tier1Economic, got %v", updated.TrustTier)
	}

	// 2. Further updates to reach Tier 3 (Sovereign)
	// (Bond >= 50, Spend >= 500, Settled >= 20, PopVerified=true, Score >= 4.5)
	updated, err = reg.UpdateEconomics(root.RootDID, 50.0, 500.0, 20, 4.8)
	if err != nil {
		t.Fatalf("UpdateEconomics failed: %v", err)
	}
	if updated.BondUSD != 100.0 || updated.CumulativeSpendUSD != 510.0 || updated.SettledTxCount != 25 {
		t.Fatalf("economic state mismatch: %+v", updated)
	}
	if updated.CompositeScore != 4.8 {
		t.Fatalf("composite score mismatch: %v", updated.CompositeScore)
	}
	if updated.TrustTier != Tier3Sovereign {
		t.Fatalf("expected promotion to Tier3Sovereign, got %v (%s)", updated.TrustTier, updated.TrustTier)
	}

	// 3. Record Slash on SubDID: rolls up to parent RootAgent, slashing bond and score!
	slashed, err := reg.RecordSlash(sub.SubDID, 80.0, 1.5)
	if err != nil {
		t.Fatalf("RecordSlash via SubDID failed: %v", err)
	}
	if slashed.SlashCount != 1 {
		t.Errorf("expected SlashCount 1, got %d", slashed.SlashCount)
	}
	if slashed.BondUSD != 20.0 { // 100 - 80 = 20
		t.Errorf("expected BondUSD 20.0, got %v", slashed.BondUSD)
	}
	if slashed.CompositeScore != 1.5 {
		t.Errorf("expected CompositeScore 1.5, got %v", slashed.CompositeScore)
	}
	// With BondUSD = 20 (< 50) and SettledTxCount = 25 (< 100), trust tier collapses to Tier 0!
	if slashed.TrustTier != Tier0Ephemeral {
		t.Errorf("expected demotion to Tier0Ephemeral, got %v (%s)", slashed.TrustTier, slashed.TrustTier)
	}

	// Verify that querying root directly reflects the slash rollup
	resolvedRoot, err := reg.ResolveRootAgent(root.RootDID)
	if err != nil {
		t.Fatalf("ResolveRootAgent failed: %v", err)
	}
	if resolvedRoot.SlashCount != 1 || resolvedRoot.BondUSD != 20.0 || resolvedRoot.TrustTier != Tier0Ephemeral {
		t.Fatalf("resolved root did not match slashed state: %+v", resolvedRoot)
	}
}

// TestListRootAndSubAgents verifies deterministic sorting and filtering of
// Root and Sub agents in Registry queries.
//
// Pre-conditions: multiple root agents and sub-agents registered.
// Post-conditions: list operations return deterministically sorted slices;
// filtering by RootDID selects only sub-agents belonging to that parent.
func TestListRootAndSubAgents(t *testing.T) {
	reg := NewRegistry()

	r1, err := reg.RegisterRootAgent("Agent 1", nil, false, false, false)
	if err != nil {
		t.Fatalf("RegisterRootAgent 1 failed: %v", err)
	}
	r2, err := reg.RegisterRootAgent("Agent 2", nil, false, false, false)
	if err != nil {
		t.Fatalf("RegisterRootAgent 2 failed: %v", err)
	}

	roots := reg.ListRootAgents()
	if len(roots) != 2 {
		t.Fatalf("expected 2 roots, got %d", len(roots))
	}
	if roots[0].RootDID > roots[1].RootDID {
		t.Fatalf("roots not sorted ascending: %s > %s", roots[0].RootDID, roots[1].RootDID)
	}

	// Spawn sub-agents
	s1_1, _ := reg.SpawnSubAgent(r1.RootDID, "r1-s1", nil, time.Hour)
	s1_2, _ := reg.SpawnSubAgent(r1.RootDID, "r1-s2", nil, time.Hour)
	s2_1, _ := reg.SpawnSubAgent(r2.RootDID, "r2-s1", nil, time.Hour)

	// List all sub-agents
	allSubs := reg.ListSubAgents("")
	if len(allSubs) != 3 {
		t.Fatalf("expected 3 sub-agents total, got %d", len(allSubs))
	}
	for i := 0; i < len(allSubs)-1; i++ {
		if allSubs[i].SubDID > allSubs[i+1].SubDID {
			t.Fatalf("allSubs not sorted ascending: %s > %s", allSubs[i].SubDID, allSubs[i+1].SubDID)
		}
	}

	// Filter by parent r1
	r1Subs := reg.ListSubAgents(r1.RootDID)
	if len(r1Subs) != 2 {
		t.Fatalf("expected 2 sub-agents for r1, got %d", len(r1Subs))
	}
	for _, s := range r1Subs {
		if s.ParentRootDID != r1.RootDID {
			t.Errorf("sub %s has wrong parent %s, expected %s", s.SubDID, s.ParentRootDID, r1.RootDID)
		}
	}

	_ = s1_1
	_ = s1_2
	_ = s2_1
}

// TestRegistryConcurrency validates thread safety under high contention
// across 32 concurrent goroutines exercising reads, registrations, spawning,
// economic deltas, and slashes.
//
// Pre-conditions: shared Registry instance.
// Post-conditions: execution completes without data races or lock deadlocks under `go test -race`.
func TestRegistryConcurrency(t *testing.T) {
	reg := NewRegistry()
	const numGoroutines = 32
	const opsPerGoroutine = 50

	var roots []*RootAgent
	for i := 0; i < 4; i++ {
		r, err := reg.RegisterRootAgent(fmt.Sprintf("Concurrent Root %d", i), nil, true, false, false)
		if err != nil {
			t.Fatalf("setup RegisterRootAgent failed: %v", err)
		}
		roots = append(roots, r)
	}

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for g := 0; g < numGoroutines; g++ {
		go func(gid int) {
			defer wg.Done()
			rootTarget := roots[gid%len(roots)]

			for i := 0; i < opsPerGoroutine; i++ {
				switch (gid + i) % 6 {
				case 0:
					// Register new agent
					_, _ = reg.RegisterRootAgent(fmt.Sprintf("Dynamic Agent %d-%d", gid, i), nil, false, false, false)
				case 1:
					// Spawn sub-agent
					_, _ = reg.SpawnSubAgent(rootTarget.RootDID, fmt.Sprintf("worker-%d-%d", gid, i), nil, 10*time.Minute)
				case 2:
					// Update economics
					_, _ = reg.UpdateEconomics(rootTarget.RootDID, 5.0, 15.0, 1, 3.5)
				case 3:
					// Record slash
					_, _ = reg.RecordSlash(rootTarget.RootDID, 2.0, 2.0)
				case 4:
					// Resolve root
					_, _ = reg.ResolveRootAgent(rootTarget.RootDID)
				case 5:
					// List queries
					_ = reg.ListRootAgents()
					_ = reg.ListSubAgents(rootTarget.RootDID)
				}
			}
		}(g)
	}

	wg.Wait()

	// Ensure registry remains consistent after concurrent bombardment
	finalRoots := reg.ListRootAgents()
	if len(finalRoots) < 4 {
		t.Fatalf("expected at least 4 roots remaining, got %d", len(finalRoots))
	}
}
