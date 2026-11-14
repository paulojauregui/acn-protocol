// Package ledger implements an append-only SHA-256 Merkle Tree ledger and
// privacy-preserving selective-disclosure trust passports for the Agent Commerce Network.
package ledger

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Sentinel errors returned by Merkle tree and ledger operations.
//
// Pre-conditions: none (compile-time package-level values).
// Post-conditions: every Merkle or ledger operation failure wraps or matches one of these errors.
var (
	// ErrEmptyTree is returned when generating an inclusion proof for an empty leaf set.
	ErrEmptyTree = errors.New("ledger: tree has no leaves")
	// ErrIndexOutOfBounds is returned when a target leaf index is outside valid bounds.
	ErrIndexOutOfBounds = errors.New("ledger: target index out of bounds")
)

// InteractionRecord represents an immutable settled interaction appended to the Merkle ledger.
//
// Pre-conditions: constructed with non-empty IDs; AmountUSD is non-negative.
// Post-conditions: LeafHash matches HashLeaf of the canonical record fields.
type InteractionRecord struct {
	RecordID      string    `json:"record_id"`
	AppointmentID string    `json:"appointment_id"`
	UserRootDID   string    `json:"user_root_did"`
	SubAgentDID   string    `json:"sub_agent_did"`
	VendorDID     string    `json:"vendor_did"`
	Status        string    `json:"status"`
	AmountUSD     float64   `json:"amount_usd"`
	Timestamp     time.Time `json:"timestamp"`
	LeafHash      string    `json:"leaf_hash"`
}

// ProofStep defines a single sibling traversal step in a Merkle inclusion proof path.
//
// Pre-conditions: SiblingHash is a 64-character lowercase hex digest.
// Post-conditions: IsLeft specifies whether the sibling is on the left when recomputing the parent hash.
type ProofStep struct {
	SiblingHash string `json:"sibling_hash"`
	IsLeft      bool   `json:"is_left"`
}

// InclusionProof provides the cryptographic evidence that a specific leaf hash is part of a Merkle root.
//
// Pre-conditions: LeafHash and MerkleRoot are 64-character hex digests; 0 <= LeafIndex.
// Post-conditions: Steps allow a verifier to reconstruct MerkleRoot using HashNodes.
type InclusionProof struct {
	LeafHash   string      `json:"leaf_hash"`
	LeafIndex  int         `json:"leaf_index"`
	MerkleRoot string      `json:"merkle_root"`
	Steps      []ProofStep `json:"steps"`
}

// HashLeaf computes the SHA-256 hex digest of leaf data prefixed with "leaf:".
//
// Pre-conditions: data is non-empty.
// Post-conditions: returns a 64-character lowercase hex digest of SHA-256("leaf:" + data).
func HashLeaf(data string) string {
	h := sha256.Sum256([]byte("leaf:" + data))
	return hex.EncodeToString(h[:])
}

// HashNodes computes the SHA-256 hex digest of two concatenated sibling nodes.
//
// Pre-conditions: leftHex and rightHex are valid hex digests.
// Post-conditions: returns a 64-character lowercase hex digest of SHA-256("node:" + leftHex + ":" + rightHex).
func HashNodes(leftHex, rightHex string) string {
	h := sha256.Sum256([]byte("node:" + leftHex + ":" + rightHex))
	return hex.EncodeToString(h[:])
}

// ComputeMerkleRoot computes the SHA-256 Merkle root of an ordered slice of leaf hashes.
//
// If leafHashes is empty, returns the SHA-256 hex digest of "empty".
// At each tree level, if the number of nodes is odd, duplicates the last node,
// then hashes adjacent pairs using HashNodes until 1 root hash remains.
//
// Pre-conditions: none.
// Post-conditions: returns a 64-character lowercase hex digest representing the Merkle root.
func ComputeMerkleRoot(leafHashes []string) string {
	if len(leafHashes) == 0 {
		h := sha256.Sum256([]byte("empty"))
		return hex.EncodeToString(h[:])
	}

	level := make([]string, len(leafHashes))
	copy(level, leafHashes)

	for len(level) > 1 {
		if len(level)%2 != 0 {
			level = append(level, level[len(level)-1])
		}
		nextLevel := make([]string, len(level)/2)
		for i := 0; i < len(level); i += 2 {
			nextLevel[i/2] = HashNodes(level[i], level[i+1])
		}
		level = nextLevel
	}

	return level[0]
}

// GenerateInclusionProof constructs an audit inclusion proof for the leaf at targetIndex.
//
// Pre-conditions: 0 <= targetIndex < len(leafHashes).
// Post-conditions: on success returns an InclusionProof whose Steps verify against ComputeMerkleRoot(leafHashes);
// on failure returns an error wrapping ErrEmptyTree or ErrIndexOutOfBounds.
func GenerateInclusionProof(leafHashes []string, targetIndex int) (*InclusionProof, error) {
	if len(leafHashes) == 0 {
		return nil, ErrEmptyTree
	}
	if targetIndex < 0 || targetIndex >= len(leafHashes) {
		return nil, fmt.Errorf("%w: target index %d with %d leaves", ErrIndexOutOfBounds, targetIndex, len(leafHashes))
	}

	targetHash := leafHashes[targetIndex]
	level := make([]string, len(leafHashes))
	copy(level, leafHashes)
	currIndex := targetIndex
	steps := make([]ProofStep, 0)

	for len(level) > 1 {
		if len(level)%2 != 0 {
			level = append(level, level[len(level)-1])
		}

		if currIndex%2 == 0 {
			siblingIndex := currIndex + 1
			steps = append(steps, ProofStep{
				SiblingHash: level[siblingIndex],
				IsLeft:      false,
			})
		} else {
			siblingIndex := currIndex - 1
			steps = append(steps, ProofStep{
				SiblingHash: level[siblingIndex],
				IsLeft:      true,
			})
		}

		nextLevel := make([]string, len(level)/2)
		for i := 0; i < len(level); i += 2 {
			nextLevel[i/2] = HashNodes(level[i], level[i+1])
		}
		level = nextLevel
		currIndex = currIndex / 2
	}

	return &InclusionProof{
		LeafHash:   targetHash,
		LeafIndex:  targetIndex,
		MerkleRoot: level[0],
		Steps:      steps,
	}, nil
}

// VerifyInclusionProof verifies an audit inclusion proof against its embedded MerkleRoot.
//
// Pre-conditions: proof may be nil or partially populated.
// Post-conditions: returns true if the proof is non-nil, non-empty, and reconstructed root matches MerkleRoot;
// returns false on any tampering, empty fields, or nil pointer.
func VerifyInclusionProof(proof *InclusionProof) bool {
	if proof == nil || proof.LeafHash == "" || proof.MerkleRoot == "" {
		return false
	}
	current := proof.LeafHash
	for _, step := range proof.Steps {
		if step.SiblingHash == "" {
			return false
		}
		if step.IsLeft {
			current = HashNodes(step.SiblingHash, current)
		} else {
			current = HashNodes(current, step.SiblingHash)
		}
	}
	return current == proof.MerkleRoot
}

// Ledger provides a concurrency-safe, append-only SHA-256 Merkle tree ledger and enclave attestation engine.
//
// Pre-conditions: constructed via NewLedger.
// Post-conditions: all operations are safe under concurrent goroutine access.
type Ledger struct {
	mu          sync.RWMutex
	records     []InteractionRecord
	leafHashes  []string
	enclavePub  ed25519.PublicKey
	enclavePriv ed25519.PrivateKey
}

// NewLedger allocates an empty Ledger initialized with a fresh Ed25519 enclave keypair.
//
// Pre-conditions: none.
// Post-conditions: the returned Ledger holds zero records, is safe for concurrent access,
// and possesses valid enclave signing and verification keys.
func NewLedger() *Ledger {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(fmt.Sprintf("ledger: failed to generate enclave ed25519 key: %v", err))
	}
	return &Ledger{
		records:     make([]InteractionRecord, 0),
		leafHashes:  make([]string, 0),
		enclavePub:  pub,
		enclavePriv: priv,
	}
}

// AppendRecord appends an interaction record to the ledger, computes its LeafHash,
// and updates the Merkle tree root.
//
// Pre-conditions: rec contains valid interaction metadata.
// Post-conditions: rec.LeafHash is populated with HashLeaf of canonical fields, rec is stored,
// and the updated CurrentMerkleRoot is returned.
func (l *Ledger) AppendRecord(rec InteractionRecord) (InteractionRecord, string, error) {
	if rec.RecordID == "" {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			return InteractionRecord{}, "", fmt.Errorf("ledger: failed to generate record id: %w", err)
		}
		rec.RecordID = "rec_" + hex.EncodeToString(b)
	}
	if rec.Timestamp.IsZero() {
		rec.Timestamp = time.Now().UTC()
	}

	rec.LeafHash = HashLeaf(fmt.Sprintf("%s|%s|%s|%s|%.2f", rec.AppointmentID, rec.UserRootDID, rec.VendorDID, rec.Status, rec.AmountUSD))

	l.mu.Lock()
	defer l.mu.Unlock()

	l.records = append(l.records, rec)
	l.leafHashes = append(l.leafHashes, rec.LeafHash)
	root := ComputeMerkleRoot(l.leafHashes)

	return rec, root, nil
}

// CurrentMerkleRoot computes and returns the SHA-256 Merkle root of all appended leaves.
//
// Pre-conditions: none.
// Post-conditions: returns a 64-character lowercase hex digest of the current Merkle root,
// or SHA-256("empty") if no records have been appended.
func (l *Ledger) CurrentMerkleRoot() string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return ComputeMerkleRoot(l.leafHashes)
}

// GetInclusionProof generates an audit inclusion proof for the leaf at index.
//
// Pre-conditions: 0 <= index < len(records).
// Post-conditions: on success returns an InclusionProof verifiable against CurrentMerkleRoot();
// on failure returns an error.
func (l *Ledger) GetInclusionProof(index int) (*InclusionProof, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return GenerateInclusionProof(l.leafHashes, index)
}

// ListRecords returns a snapshot copy of all interaction records in the ledger.
//
// Pre-conditions: none.
// Post-conditions: returns a newly allocated slice of InteractionRecord copies; mutating
// the returned slice does not modify internal ledger state.
func (l *Ledger) ListRecords() []InteractionRecord {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]InteractionRecord, len(l.records))
	copy(out, l.records)
	return out
}

// EnclavePublicKey returns a defensive copy of the ledger's enclave Ed25519 public key.
//
// Pre-conditions: none.
// Post-conditions: returns a 32-byte Ed25519 public key.
func (l *Ledger) EnclavePublicKey() ed25519.PublicKey {
	l.mu.RLock()
	defer l.mu.RUnlock()
	pub := make(ed25519.PublicKey, len(l.enclavePub))
	copy(pub, l.enclavePub)
	return pub
}
