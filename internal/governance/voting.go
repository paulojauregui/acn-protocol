// Package governance implements the ACN Protocol Constitution, reputation-weighted
// governance voting with validator veto mechanics, and the hybrid AI audit jury.
package governance

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"
)

// Sentinel errors returned by governance voting operations.
//
// Pre-conditions: none (package-level immutable error values).
// Post-conditions: returned when input validation fails or requested entities are missing.
var (
	// ErrEmptyProposalID is returned when a proposal identifier is blank.
	ErrEmptyProposalID = errors.New("governance: proposal id must not be empty")
	// ErrEmptyProposalTitle is returned when a proposal title is blank.
	ErrEmptyProposalTitle = errors.New("governance: proposal title must not be empty")
	// ErrEmptyVoterDID is returned when a voter DID is blank.
	ErrEmptyVoterDID = errors.New("governance: voter did must not be empty")
	// ErrProposalNotFound is returned when operating on a non-existent proposal.
	ErrProposalNotFound = errors.New("governance: proposal not found")
	// ErrProposalAlreadyExists is returned when attempting to create a proposal with an existing ID.
	ErrProposalAlreadyExists = errors.New("governance: proposal already exists")
	// ErrAlreadyVoted is returned when a voter attempts to cast a duplicate vote on the same proposal.
	ErrAlreadyVoted = errors.New("governance: voter has already cast a vote on this proposal")
)

// Constitution specifies the fundamental protocol governance parameters.
//
// Pre-conditions: initialized via DefaultConstitution or explicit configuration.
// Post-conditions: numeric rates and shares remain bounded within valid economic ranges.
type Constitution struct {
	NetworkIntegrityFeeRate    float64 `json:"network_integrity_fee_rate"`
	VendorSlashShare           float64 `json:"vendor_slash_share"`
	NetworkSlashShare          float64 `json:"network_slash_share"`
	ProbationaryScoreThreshold float64 `json:"probationary_score_threshold"`
	NonceTTLSeconds            int     `json:"nonce_ttl_seconds"`
}

// DefaultConstitution returns the canonical constitution parameter configuration.
//
// Pre-conditions: none.
// Post-conditions: returns a Constitution instance populated with protocol defaults.
func DefaultConstitution() Constitution {
	return Constitution{
		NetworkIntegrityFeeRate:    0.005,
		VendorSlashShare:           0.70,
		NetworkSlashShare:          0.30,
		ProbationaryScoreThreshold: 2.50,
		NonceTTLSeconds:            60,
	}
}

// ProposalStatus represents the lifecycle state of a governance proposal.
//
// Pre-conditions: must be one of the defined ProposalStatus constants.
// Post-conditions: reflects current resolution or active voting status.
type ProposalStatus string

const (
	// ProposalActive indicates voting is open and no final resolution has been reached.
	ProposalActive ProposalStatus = "ACTIVE"
	// ProposalPassed indicates yes vote weight strictly exceeds no vote weight without validator veto.
	ProposalPassed ProposalStatus = "PASSED"
	// ProposalRejected indicates no vote weight equals or exceeds yes vote weight.
	ProposalRejected ProposalStatus = "REJECTED"
	// ProposalVetoed indicates a validator exercised absolute veto authority over the proposal.
	ProposalVetoed ProposalStatus = "VETOED_BY_VALIDATORS"
)

// VoteReceipt records the receipt and weight calculation of an individual cast vote.
//
// Pre-conditions: ProposalID and VoterDID must be non-empty; StakeUSD and ReputationScore non-negative.
// Post-conditions: VoteWeight equals ComputeVoteWeight(StakeUSD, ReputationScore).
type VoteReceipt struct {
	ProposalID      string  `json:"proposal_id"`
	VoterDID        string  `json:"voter_did"`
	StakeUSD        float64 `json:"stake_usd"`
	ReputationScore float64 `json:"reputation_score"`
	VoteWeight      float64 `json:"vote_weight"`
	Support         bool    `json:"support"`
	ValidatorVeto   bool    `json:"validator_veto"`
}

// Proposal encapsulates a governance parameter change proposal and its aggregated votes.
//
// Pre-conditions: ProposalID and Title must be non-empty strings.
// Post-conditions: YesWeight and NoWeight are non-negative; Status reflects vote tally and veto state.
type Proposal struct {
	ProposalID      string         `json:"proposal_id"`
	Title           string         `json:"title"`
	Description     string         `json:"description"`
	ParameterKey    string         `json:"parameter_key"`
	ProposedValue   string         `json:"proposed_value"`
	YesWeight       float64        `json:"yes_weight"`
	NoWeight        float64        `json:"no_weight"`
	ValidatorVetoed bool           `json:"validator_vetoed"`
	Status          ProposalStatus `json:"status"`
	CreatedAt       time.Time      `json:"created_at"`
}

// ComputeVoteWeight calculates the governance voting power using the logarithmic stake and reputation formula:
// V_i = ln(1 + S_i) * R_i.
//
// Pre-conditions: stakeUSD >= 0 (clamped if negative); reputationScore in [0.0, 5.0] (clamped if outside).
// Post-conditions: returns 0.0 if stakeUSD <= 0 or reputationScore <= 0; returns strictly positive weight otherwise.
func ComputeVoteWeight(stakeUSD, reputationScore float64) float64 {
	effectiveStake := math.Max(0.0, stakeUSD)
	effectiveReputation := math.Max(0.0, math.Min(5.0, reputationScore))
	if effectiveStake == 0.0 || effectiveReputation == 0.0 {
		return 0.0
	}
	return math.Log1p(effectiveStake) * effectiveReputation
}

// Engine manages protocol governance proposals, constitution parameters, and vote tallies.
//
// Pre-conditions: initialized via NewEngine.
// Post-conditions: concurrency-safe via internal RWMutex; preserves state invariants under race conditions.
type Engine struct {
	mu           sync.RWMutex
	constitution Constitution
	proposals    map[string]*Proposal
	votes        map[string]map[string]VoteReceipt
}

// NewEngine allocates and initializes a new governance Engine with default constitution.
//
// Pre-conditions: none.
// Post-conditions: returns a ready-to-use Engine holding zero proposals and zero votes.
func NewEngine() *Engine {
	return &Engine{
		constitution: DefaultConstitution(),
		proposals:    make(map[string]*Proposal),
		votes:        make(map[string]map[string]VoteReceipt),
	}
}

// GetConstitution returns a copy of the active governance constitution.
//
// Pre-conditions: none.
// Post-conditions: returns the active Constitution safely under read lock.
func (e *Engine) GetConstitution() Constitution {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.constitution
}

// CreateProposal registers a new governance parameter change proposal.
//
// Pre-conditions: id and title must be non-empty; id must not already exist in Engine.
// Post-conditions: on success, stores proposal with ProposalActive status and returns a deep copy.
func (e *Engine) CreateProposal(id, title, description, paramKey, proposedVal string) (*Proposal, error) {
	if id == "" {
		return nil, ErrEmptyProposalID
	}
	if title == "" {
		return nil, ErrEmptyProposalTitle
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if _, exists := e.proposals[id]; exists {
		return nil, fmt.Errorf("%w: %s", ErrProposalAlreadyExists, id)
	}

	p := &Proposal{
		ProposalID:      id,
		Title:           title,
		Description:     description,
		ParameterKey:    paramKey,
		ProposedValue:   proposedVal,
		YesWeight:       0.0,
		NoWeight:        0.0,
		ValidatorVetoed: false,
		Status:          ProposalActive,
		CreatedAt:       time.Now().UTC(),
	}

	e.proposals[id] = p
	e.votes[id] = make(map[string]VoteReceipt)

	clone := *p
	return &clone, nil
}

// CastVote records an individual vote on an existing proposal and recalculates proposal status.
//
// Pre-conditions: proposalID and voterDID must be non-empty; proposal must exist; voter must not have voted.
// Post-conditions: on success, records VoteReceipt, updates YesWeight/NoWeight or sets ValidatorVetoed,
// resolves Proposal status, and returns copies of VoteReceipt and updated Proposal.
func (e *Engine) CastVote(proposalID, voterDID string, stakeUSD, reputationScore float64, support, isValidatorVeto bool) (*VoteReceipt, *Proposal, error) {
	if proposalID == "" {
		return nil, nil, ErrEmptyProposalID
	}
	if voterDID == "" {
		return nil, nil, ErrEmptyVoterDID
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	prop, ok := e.proposals[proposalID]
	if !ok {
		return nil, nil, fmt.Errorf("%w: %s", ErrProposalNotFound, proposalID)
	}

	proposalVotes := e.votes[proposalID]
	if _, alreadyVoted := proposalVotes[voterDID]; alreadyVoted {
		return nil, nil, fmt.Errorf("%w: voter %s on proposal %s", ErrAlreadyVoted, voterDID, proposalID)
	}

	weight := ComputeVoteWeight(stakeUSD, reputationScore)

	receipt := VoteReceipt{
		ProposalID:      proposalID,
		VoterDID:        voterDID,
		StakeUSD:        stakeUSD,
		ReputationScore: reputationScore,
		VoteWeight:      weight,
		Support:         support,
		ValidatorVeto:   isValidatorVeto,
	}

	if isValidatorVeto {
		prop.ValidatorVetoed = true
		prop.Status = ProposalVetoed
	} else {
		if support {
			prop.YesWeight += weight
		} else {
			prop.NoWeight += weight
		}

		if prop.ValidatorVetoed {
			prop.Status = ProposalVetoed
		} else if prop.YesWeight > prop.NoWeight {
			prop.Status = ProposalPassed
		} else if prop.NoWeight > prop.YesWeight {
			prop.Status = ProposalRejected
		} else {
			prop.Status = ProposalActive
		}
	}

	proposalVotes[voterDID] = receipt

	rCopy := receipt
	pCopy := *prop
	return &rCopy, &pCopy, nil
}

// ListProposals returns a list of all registered proposals sorted deterministically by ProposalID.
//
// Pre-conditions: none.
// Post-conditions: returns deep copies of all proposals ordered lexicographically by ProposalID.
func (e *Engine) ListProposals() []*Proposal {
	e.mu.RLock()
	defer e.mu.RUnlock()

	keys := make([]string, 0, len(e.proposals))
	for k := range e.proposals {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	result := make([]*Proposal, 0, len(keys))
	for _, k := range keys {
		clone := *e.proposals[k]
		result = append(result, &clone)
	}
	return result
}
