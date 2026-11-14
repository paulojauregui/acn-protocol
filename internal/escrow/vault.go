package escrow

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Vault manages Good-Behavior Bond accounts and blind escrow locks.
//
// Pre-conditions: must be initialized via NewVault before concurrent use.
// Post-conditions: all operations are thread-safe and maintain ledger consistency.
type Vault struct {
	mu              sync.RWMutex
	accounts        map[string]*BondAccount
	locks           map[string]*EscrowLock
	treasuryFeesUSD float64
	totalSlashedUSD float64
}

// NewVault constructs and initializes an empty escrow Vault.
//
// Pre-conditions: none.
// Post-conditions: returns a ready-to-use Vault instance with empty ledger maps.
func NewVault() *Vault {
	return &Vault{
		accounts: make(map[string]*BondAccount),
		locks:    make(map[string]*EscrowLock),
	}
}

// DepositBond adds capital to an agent's available bond balance.
//
// Pre-conditions: rootDID must be non-empty; amountUSD must be strictly positive.
// Post-conditions: AvailableBondUSD of rootDID is incremented by RoundCents(amountUSD).
func (v *Vault) DepositBond(rootDID string, amountUSD float64) (*BondAccount, error) {
	if rootDID == "" {
		return nil, ErrInvalidDID
	}
	if amountUSD <= 0 {
		return nil, ErrInvalidAmount
	}

	rounded := RoundCents(amountUSD)
	now := time.Now().UTC()

	v.mu.Lock()
	defer v.mu.Unlock()

	acc, exists := v.accounts[rootDID]
	if !exists {
		acc = &BondAccount{
			RootDID: rootDID,
		}
		v.accounts[rootDID] = acc
	}

	acc.AvailableBondUSD = RoundCents(acc.AvailableBondUSD + rounded)
	acc.UpdatedAt = now

	return cloneAccount(acc), nil
}

// RecordStripeSpend updates the proof-of-spend ledger for an agent.
//
// Pre-conditions: rootDID must be non-empty; spendUSD and txCountDelta must be non-negative.
// Post-conditions: CumulativeSpendUSD and SettledTxCount are incremented accordingly.
func (v *Vault) RecordStripeSpend(rootDID string, spendUSD float64, txCountDelta int) (*BondAccount, error) {
	if rootDID == "" {
		return nil, ErrInvalidDID
	}
	if spendUSD < 0 {
		return nil, errors.New("spendUSD must be non-negative")
	}
	if txCountDelta < 0 {
		return nil, errors.New("txCountDelta must be non-negative")
	}

	roundedSpend := RoundCents(spendUSD)
	now := time.Now().UTC()

	v.mu.Lock()
	defer v.mu.Unlock()

	acc, exists := v.accounts[rootDID]
	if !exists {
		acc = &BondAccount{
			RootDID: rootDID,
		}
		v.accounts[rootDID] = acc
	}

	acc.CumulativeSpendUSD = RoundCents(acc.CumulativeSpendUSD + roundedSpend)
	acc.SettledTxCount += txCountDelta
	acc.UpdatedAt = now

	return cloneAccount(acc), nil
}

// GetAccount retrieves a point-in-time snapshot of an agent's bond account.
//
// Pre-conditions: rootDID must be non-empty.
// Post-conditions: returns a cloned copy of BondAccount or ErrAccountNotFound.
func (v *Vault) GetAccount(rootDID string) (*BondAccount, error) {
	if rootDID == "" {
		return nil, ErrInvalidDID
	}

	v.mu.RLock()
	defer v.mu.RUnlock()

	acc, exists := v.accounts[rootDID]
	if !exists {
		return nil, ErrAccountNotFound
	}

	return cloneAccount(acc), nil
}

// LockBond freezes bond collateral for a pending appointment in blind escrow.
//
// Pre-conditions: appointmentID, payerRootDID, and vendorDID must be non-empty;
// amountUSD must be strictly positive; payer must have sufficient available bond.
// Post-conditions: amountUSD is moved from AvailableBondUSD to LockedBondUSD,
// and an EscrowLock in LockStatusActive is created.
func (v *Vault) LockBond(appointmentID, payerRootDID, callerDID, vendorDID string, amountUSD float64) (*EscrowLock, error) {
	if appointmentID == "" {
		return nil, ErrInvalidAppointmentID
	}
	if payerRootDID == "" || vendorDID == "" {
		return nil, ErrInvalidDID
	}
	if amountUSD <= 0 {
		return nil, ErrInvalidAmount
	}

	rounded := RoundCents(amountUSD)
	now := time.Now().UTC()

	v.mu.Lock()
	defer v.mu.Unlock()

	for _, l := range v.locks {
		if l.AppointmentID == appointmentID && l.Status == LockStatusActive {
			return nil, ErrActiveLockExists
		}
	}

	payer, exists := v.accounts[payerRootDID]
	if !exists || payer.AvailableBondUSD < rounded {
		return nil, ErrInsufficientBond
	}

	payer.AvailableBondUSD = RoundCents(payer.AvailableBondUSD - rounded)
	payer.LockedBondUSD = RoundCents(payer.LockedBondUSD + rounded)
	payer.UpdatedAt = now

	h := sha256.New()
	fmt.Fprintf(h, "%s:%s:%s:%s:%d", appointmentID, payerRootDID, callerDID, vendorDID, now.UnixNano())
	lockID := "lock-" + hex.EncodeToString(h.Sum(nil))[:24]

	lock := &EscrowLock{
		LockID:        lockID,
		AppointmentID: appointmentID,
		PayerRootDID:  payerRootDID,
		CallerDID:     callerDID,
		VendorDID:     vendorDID,
		AmountUSD:     rounded,
		Status:        LockStatusActive,
		CreatedAt:     now,
	}

	v.locks[lockID] = lock

	return cloneLock(lock), nil
}

// ReleaseBond settles an escrow lock, deducting the 0.5% network fee and returning collateral.
//
// Pre-conditions: lockID must exist and have Status == LockStatusActive.
// Post-conditions: locked amount minus 0.5% fee is returned to AvailableBondUSD;
// SettledTxCount is incremented by 1; lock status is set to LockStatusReleased.
func (v *Vault) ReleaseBond(lockID string) (*SettlementReceipt, error) {
	if lockID == "" {
		return nil, ErrLockNotFound
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	lock, exists := v.locks[lockID]
	if !exists {
		return nil, ErrLockNotFound
	}
	if lock.Status != LockStatusActive {
		return nil, ErrLockNotActive
	}

	fee := RoundCents(lock.AmountUSD * NetworkIntegrityFeeRate)
	released := RoundCents(lock.AmountUSD - fee)
	now := time.Now().UTC()

	payer, exists := v.accounts[lock.PayerRootDID]
	if !exists {
		payer = &BondAccount{RootDID: lock.PayerRootDID}
		v.accounts[lock.PayerRootDID] = payer
	}

	payer.LockedBondUSD = RoundCents(payer.LockedBondUSD - lock.AmountUSD)
	payer.AvailableBondUSD = RoundCents(payer.AvailableBondUSD + released)
	payer.SettledTxCount++
	payer.CumulativeSpendUSD = RoundCents(payer.CumulativeSpendUSD + lock.AmountUSD)
	payer.UpdatedAt = now

	v.treasuryFeesUSD = RoundCents(v.treasuryFeesUSD + fee)

	lock.Status = LockStatusReleased
	lock.NetworkFeeUSD = fee
	lock.VendorPayoutUSD = 0.0
	lock.SettledAt = now

	receipt := &SettlementReceipt{
		LockID:                      lock.LockID,
		AppointmentID:               lock.AppointmentID,
		Status:                      LockStatusReleased,
		ReleasedToUserUSD:           released,
		NetworkIntegrityFeeUSD:      fee,
		VendorCompensationUSD:       0.0,
		NetworkArbitrationFeeUSD:    0.0,
		GoodBehaviorBondDeductedUSD: 0.0,
	}

	return receipt, nil
}

// SlashBond forfeits locked collateral and optional penalty, splitting 70% to vendor and 30% to treasury.
//
// Pre-conditions: lockID must exist with Status == LockStatusActive; extraPenaltyUSD must be non-negative.
// Post-conditions: 100% of locked collateral and up to extraPenaltyUSD from available bond are slashed;
// vendor receives 70% compensation and treasury receives 30% arbitration fee; lock marked LockStatusSlashed.
func (v *Vault) SlashBond(lockID string, extraPenaltyUSD float64) (*SettlementReceipt, error) {
	if lockID == "" {
		return nil, ErrLockNotFound
	}
	if extraPenaltyUSD < 0 {
		return nil, ErrNegativeExtraPenalty
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	lock, exists := v.locks[lockID]
	if !exists {
		return nil, ErrLockNotFound
	}
	if lock.Status != LockStatusActive {
		return nil, ErrLockNotActive
	}

	now := time.Now().UTC()

	payer, exists := v.accounts[lock.PayerRootDID]
	if !exists {
		payer = &BondAccount{RootDID: lock.PayerRootDID}
		v.accounts[lock.PayerRootDID] = payer
	}

	payer.LockedBondUSD = RoundCents(payer.LockedBondUSD - lock.AmountUSD)

	deductedExtra := 0.0
	if extraPenaltyUSD > 0 {
		penalty := RoundCents(extraPenaltyUSD)
		if payer.AvailableBondUSD < penalty {
			deductedExtra = payer.AvailableBondUSD
			payer.AvailableBondUSD = 0.0
		} else {
			deductedExtra = penalty
			payer.AvailableBondUSD = RoundCents(payer.AvailableBondUSD - penalty)
		}
	}

	payer.TotalSlashedUSD = RoundCents(payer.TotalSlashedUSD + lock.AmountUSD + deductedExtra)
	payer.UpdatedAt = now

	totalPool := RoundCents(lock.AmountUSD + deductedExtra)
	v.totalSlashedUSD = RoundCents(v.totalSlashedUSD + totalPool)

	vendorComp := RoundCents(totalPool * VendorSlashCompensationRate)
	arbFee := RoundCents(totalPool - vendorComp)
	v.treasuryFeesUSD = RoundCents(v.treasuryFeesUSD + arbFee)

	vendor, exists := v.accounts[lock.VendorDID]
	if !exists {
		vendor = &BondAccount{RootDID: lock.VendorDID}
		v.accounts[lock.VendorDID] = vendor
	}
	vendor.VendorRewardsUSD = RoundCents(vendor.VendorRewardsUSD + vendorComp)
	vendor.AvailableBondUSD = RoundCents(vendor.AvailableBondUSD + vendorComp)
	vendor.UpdatedAt = now

	lock.Status = LockStatusSlashed
	lock.NetworkFeeUSD = arbFee
	lock.VendorPayoutUSD = vendorComp
	lock.SettledAt = now

	receipt := &SettlementReceipt{
		LockID:                      lock.LockID,
		AppointmentID:               lock.AppointmentID,
		Status:                      LockStatusSlashed,
		ReleasedToUserUSD:           0.0,
		NetworkIntegrityFeeUSD:      0.0,
		VendorCompensationUSD:       vendorComp,
		NetworkArbitrationFeeUSD:    arbFee,
		GoodBehaviorBondDeductedUSD: deductedExtra,
	}

	return receipt, nil
}

// GetLock retrieves an escrow lock snapshot by its identifier.
//
// Pre-conditions: lockID must be non-empty.
// Post-conditions: returns a cloned copy of EscrowLock or ErrLockNotFound.
func (v *Vault) GetLock(lockID string) (*EscrowLock, error) {
	if lockID == "" {
		return nil, ErrLockNotFound
	}

	v.mu.RLock()
	defer v.mu.RUnlock()

	lock, exists := v.locks[lockID]
	if !exists {
		return nil, ErrLockNotFound
	}

	return cloneLock(lock), nil
}

// ListLocks returns a cloned snapshot of all escrow locks ordered by creation time.
//
// Pre-conditions: none.
// Post-conditions: returns a slice of EscrowLock pointers ordered by CreatedAt ascending.
func (v *Vault) ListLocks() []*EscrowLock {
	v.mu.RLock()
	defer v.mu.RUnlock()

	result := make([]*EscrowLock, 0, len(v.locks))
	for _, l := range v.locks {
		result = append(result, cloneLock(l))
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].LockID < result[j].LockID
		}
		return result[i].CreatedAt.Before(result[j].CreatedAt)
	})

	return result
}

// Stats returns aggregate system metrics across all accounts and treasury reserves.
//
// Pre-conditions: none.
// Post-conditions: returns rounded totals for locked bond, available bond, treasury fees, and total slashed capital.
func (v *Vault) Stats() (totalLockedUSD, totalAvailableUSD, treasuryFeesUSD, totalSlashedUSD float64) {
	v.mu.RLock()
	defer v.mu.RUnlock()

	for _, acc := range v.accounts {
		totalAvailableUSD += acc.AvailableBondUSD
		totalLockedUSD += acc.LockedBondUSD
	}

	return RoundCents(totalLockedUSD), RoundCents(totalAvailableUSD), RoundCents(v.treasuryFeesUSD), RoundCents(v.totalSlashedUSD)
}

func cloneAccount(a *BondAccount) *BondAccount {
	if a == nil {
		return nil
	}
	clone := *a
	return &clone
}

func cloneLock(l *EscrowLock) *EscrowLock {
	if l == nil {
		return nil
	}
	clone := *l
	return &clone
}
