// Package escrow implements Good-Behavior Bond accounts, Stripe ACS
// Proof-of-Spend ledger tracking, and blind escrow locks with network
// fee and slash distributions for the Agent Communication Network (ACN).
package escrow

import (
	"errors"
	"math"
	"time"
)

// NetworkIntegrityFeeRate is the 0.5% protocol fee retained on successful release.
//
// Pre-conditions: none (compile-time constant).
// Post-conditions: exactly 0.005 (0.5%).
const NetworkIntegrityFeeRate = 0.005

// VendorSlashCompensationRate is the 70% share of slashed funds paid to the vendor.
//
// Pre-conditions: none (compile-time constant).
// Post-conditions: exactly 0.70 (70.0%).
const VendorSlashCompensationRate = 0.70

// NetworkArbitrationFeeRate is the 30% share of slashed funds retained by the treasury.
//
// Pre-conditions: none (compile-time constant).
// Post-conditions: exactly 0.30 (30.0%).
const NetworkArbitrationFeeRate = 0.30

// Common error definitions for escrow and bond operations.
//
// Pre-conditions: none (sentinel error definitions).
// Post-conditions: errors are immutable, non-nil sentinels.
var (
	// ErrAccountNotFound indicates that no bond account exists for the specified DID.
	ErrAccountNotFound = errors.New("bond account not found")

	// ErrLockNotFound indicates that the requested escrow lock identifier does not exist.
	ErrLockNotFound = errors.New("escrow lock not found")

	// ErrLockNotActive indicates that the lock has already been settled or slashed.
	ErrLockNotActive = errors.New("escrow lock is not active")

	// ErrInsufficientBond indicates that the account lacks sufficient available bond.
	ErrInsufficientBond = errors.New("insufficient available bond")

	// ErrInvalidAmount indicates that the monetary amount supplied is non-positive.
	ErrInvalidAmount = errors.New("amount must be strictly positive")

	// ErrInvalidDID indicates that a required DID identifier was empty.
	ErrInvalidDID = errors.New("DID cannot be empty")

	// ErrInvalidAppointmentID indicates that a required appointment identifier was empty.
	ErrInvalidAppointmentID = errors.New("appointment ID cannot be empty")

	// ErrNegativeExtraPenalty indicates that a negative penalty value was supplied.
	ErrNegativeExtraPenalty = errors.New("extra penalty cannot be negative")

	// ErrActiveLockExists indicates that an active lock already exists for this appointment.
	ErrActiveLockExists = errors.New("active lock already exists for appointment")
)

// LockStatus represents the lifecycle state of a blind escrow lock.
//
// Pre-conditions: valid states are LockStatusActive, LockStatusReleased, or LockStatusSlashed.
// Post-conditions: string representation of the escrow lock status.
type LockStatus string

const (
	// LockStatusActive represents an escrow lock currently holding collateral.
	LockStatusActive LockStatus = "LOCKED"

	// LockStatusReleased represents an escrow lock that settled successfully.
	LockStatusReleased LockStatus = "RELEASED"

	// LockStatusSlashed represents an escrow lock that was forfeited due to violation.
	LockStatusSlashed LockStatus = "SLASHED"
)

// BondAccount records the capital commitments, proof of spend, and reward balances of an agent.
//
// Pre-conditions: RootDID must be non-empty; monetary amounts must be non-negative.
// Post-conditions: balances accurately represent the ledger state for RootDID.
type BondAccount struct {
	RootDID            string    `json:"root_did"`
	AvailableBondUSD   float64   `json:"available_bond_usd"`
	LockedBondUSD      float64   `json:"locked_bond_usd"`
	CumulativeSpendUSD float64   `json:"cumulative_spend_usd"`
	SettledTxCount     int       `json:"settled_tx_count"`
	TotalSlashedUSD    float64   `json:"total_slashed_usd"`
	VendorRewardsUSD   float64   `json:"vendor_rewards_usd"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// EscrowLock represents collateral held in blind escrow for a specific appointment.
//
// Pre-conditions: LockID, AppointmentID, PayerRootDID, and VendorDID must be non-empty.
// Post-conditions: AmountUSD is locked from PayerRootDID until released or slashed.
type EscrowLock struct {
	LockID          string     `json:"lock_id"`
	AppointmentID   string     `json:"appointment_id"`
	PayerRootDID    string     `json:"payer_root_did"`
	CallerDID       string     `json:"caller_did"`
	VendorDID       string     `json:"vendor_did"`
	AmountUSD       float64    `json:"amount_usd"`
	Status          LockStatus `json:"status"`
	NetworkFeeUSD   float64    `json:"network_fee_usd"`
	VendorPayoutUSD float64    `json:"vendor_payout_usd"`
	CreatedAt       time.Time  `json:"created_at"`
	SettledAt       time.Time  `json:"settled_at"`
}

// SettlementReceipt details the financial outcome of an escrow lock resolution.
//
// Pre-conditions: LockID and AppointmentID must match the resolved EscrowLock.
// Post-conditions: all debited and credited amounts sum to the total liquidated pool.
type SettlementReceipt struct {
	LockID                      string     `json:"lock_id"`
	AppointmentID               string     `json:"appointment_id"`
	Status                      LockStatus `json:"status"`
	ReleasedToUserUSD           float64    `json:"released_to_user_usd"`
	NetworkIntegrityFeeUSD      float64    `json:"network_integrity_fee_usd"`
	VendorCompensationUSD       float64    `json:"vendor_compensation_usd"`
	NetworkArbitrationFeeUSD    float64    `json:"network_arbitration_fee_usd"`
	GoodBehaviorBondDeductedUSD float64    `json:"good_behavior_bond_deducted_usd"`
}

// RoundCents rounds a monetary amount to 2 decimal places using math.Round.
//
// Pre-conditions: amount is a finite float64 monetary value.
// Post-conditions: returns amount rounded to the nearest cent (10^-2).
func RoundCents(amount float64) float64 {
	return math.Round(amount*100.0) / 100.0
}
