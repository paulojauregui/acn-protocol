package escrow_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/paulojauregui/acn-protocol/internal/escrow"
)

// TestDepositAndStripeSpend tests bond deposits, proof-of-spend recording, and error conditions.
//
// Pre-conditions: fresh Vault instance.
// Post-conditions: deposits and spend updates are accurately reflected in the BondAccount.
func TestDepositAndStripeSpend(t *testing.T) {
	v := escrow.NewVault()
	did := "did:key:z6MkpTHR8VNsBxYAAWHut2Geadd9jSwuBV8xRoAnwWsdvktH"

	// Initial query should return ErrAccountNotFound.
	if _, err := v.GetAccount(did); err != escrow.ErrAccountNotFound {
		t.Fatalf("expected ErrAccountNotFound, got %v", err)
	}
	if _, err := v.GetAccount(""); err != escrow.ErrInvalidDID {
		t.Fatalf("expected ErrInvalidDID for empty rootDID, got %v", err)
	}

	// Validation on invalid inputs.
	if _, err := v.DepositBond("", 100.0); err != escrow.ErrInvalidDID {
		t.Fatalf("expected ErrInvalidDID for empty rootDID, got %v", err)
	}
	if _, err := v.DepositBond(did, 0.0); err != escrow.ErrInvalidAmount {
		t.Fatalf("expected ErrInvalidAmount for 0.0 deposit, got %v", err)
	}
	if _, err := v.DepositBond(did, -50.0); err != escrow.ErrInvalidAmount {
		t.Fatalf("expected ErrInvalidAmount for negative deposit, got %v", err)
	}

	// Valid deposit.
	acc, err := v.DepositBond(did, 250.755)
	if err != nil {
		t.Fatalf("DepositBond failed: %v", err)
	}
	if acc.AvailableBondUSD != 250.76 {
		t.Fatalf("expected 250.76 AvailableBondUSD, got %f", acc.AvailableBondUSD)
	}

	// Record Stripe Spend on existing account.
	if _, err := v.RecordStripeSpend("", 50.0, 1); err != escrow.ErrInvalidDID {
		t.Fatalf("expected ErrInvalidDID for empty rootDID on spend, got %v", err)
	}
	if _, err := v.RecordStripeSpend(did, -10.0, 1); err == nil {
		t.Fatalf("expected error for negative spend")
	}
	if _, err := v.RecordStripeSpend(did, 10.0, -1); err == nil {
		t.Fatalf("expected error for negative txCountDelta")
	}

	acc2, err := v.RecordStripeSpend(did, 120.50, 3)
	if err != nil {
		t.Fatalf("RecordStripeSpend failed: %v", err)
	}
	if acc2.CumulativeSpendUSD != 120.50 {
		t.Fatalf("expected 120.50 CumulativeSpendUSD, got %f", acc2.CumulativeSpendUSD)
	}
	if acc2.SettledTxCount != 3 {
		t.Fatalf("expected 3 SettledTxCount, got %d", acc2.SettledTxCount)
	}

	// Record Stripe Spend on brand new account without previous deposit.
	brandNewDID := "did:key:brandNewAgent"
	accNew, err := v.RecordStripeSpend(brandNewDID, 55.25, 2)
	if err != nil {
		t.Fatalf("RecordStripeSpend on brand new account failed: %v", err)
	}
	if accNew.CumulativeSpendUSD != 55.25 || accNew.SettledTxCount != 2 {
		t.Fatalf("expected 55.25 and 2 count for new account, got %+v", accNew)
	}

	// Verify GetAccount returns consistent snapshot.
	fetched, err := v.GetAccount(did)
	if err != nil {
		t.Fatalf("GetAccount failed: %v", err)
	}
	if fetched.AvailableBondUSD != 250.76 || fetched.CumulativeSpendUSD != 120.50 || fetched.SettledTxCount != 3 {
		t.Fatalf("fetched account snapshot mismatch: %+v", fetched)
	}
}

// TestLockBondInsufficientAndValid tests locking collateral and balance transfers.
//
// Pre-conditions: account with funded AvailableBondUSD.
// Post-conditions: funds are transferred to LockedBondUSD and active lock is created.
func TestLockBondInsufficientAndValid(t *testing.T) {
	v := escrow.NewVault()
	payerDID := "did:key:payerRoot111"
	vendorDID := "did:key:vendor222"
	callerDID := "did:key:caller333"

	// Rejection on non-existent account or 0 balance.
	_, err := v.LockBond("apt-01", payerDID, callerDID, vendorDID, 50.0)
	if err != escrow.ErrInsufficientBond {
		t.Fatalf("expected ErrInsufficientBond for uninitialized account, got %v", err)
	}

	// Fund payer with $100.
	if _, err := v.DepositBond(payerDID, 100.0); err != nil {
		t.Fatalf("DepositBond failed: %v", err)
	}

	// Validation checks.
	if _, err := v.LockBond("", payerDID, callerDID, vendorDID, 50.0); err != escrow.ErrInvalidAppointmentID {
		t.Fatalf("expected ErrInvalidAppointmentID, got %v", err)
	}
	if _, err := v.LockBond("apt-01", "", callerDID, vendorDID, 50.0); err != escrow.ErrInvalidDID {
		t.Fatalf("expected ErrInvalidDID for empty payer, got %v", err)
	}
	if _, err := v.LockBond("apt-01", payerDID, callerDID, "", 50.0); err != escrow.ErrInvalidDID {
		t.Fatalf("expected ErrInvalidDID for empty vendor, got %v", err)
	}
	if _, err := v.LockBond("apt-01", payerDID, callerDID, vendorDID, 0.0); err != escrow.ErrInvalidAmount {
		t.Fatalf("expected ErrInvalidAmount for 0.0 amount, got %v", err)
	}

	// Exceed available funds ($150 > $100).
	if _, err := v.LockBond("apt-01", payerDID, callerDID, vendorDID, 150.0); err != escrow.ErrInsufficientBond {
		t.Fatalf("expected ErrInsufficientBond for exceeding balance, got %v", err)
	}

	// Successful lock.
	lock, err := v.LockBond("apt-01", payerDID, callerDID, vendorDID, 60.0)
	if err != nil {
		t.Fatalf("LockBond failed: %v", err)
	}
	if lock.Status != escrow.LockStatusActive {
		t.Fatalf("expected LockStatusActive, got %s", lock.Status)
	}
	if lock.AmountUSD != 60.0 {
		t.Fatalf("expected 60.0 AmountUSD, got %f", lock.AmountUSD)
	}

	// Attempt duplicate active lock for same appointment.
	if _, err := v.LockBond("apt-01", payerDID, callerDID, vendorDID, 10.0); err != escrow.ErrActiveLockExists {
		t.Fatalf("expected ErrActiveLockExists for same active appointment ID, got %v", err)
	}

	payerAcc, err := v.GetAccount(payerDID)
	if err != nil {
		t.Fatalf("GetAccount failed: %v", err)
	}
	if payerAcc.AvailableBondUSD != 40.0 {
		t.Fatalf("expected 40.0 AvailableBondUSD, got %f", payerAcc.AvailableBondUSD)
	}
	if payerAcc.LockedBondUSD != 60.0 {
		t.Fatalf("expected 60.0 LockedBondUSD, got %f", payerAcc.LockedBondUSD)
	}
}

// TestReleaseBondFeeAndReclaim tests the 0.5% network integrity fee and collateral return.
//
// Pre-conditions: active EscrowLock with locked collateral.
// Post-conditions: 0.5% fee routed to treasury, 99.5% returned to payer, SettledTxCount incremented.
func TestReleaseBondFeeAndReclaim(t *testing.T) {
	v := escrow.NewVault()
	payerDID := "did:key:payerRoot444"
	vendorDID := "did:key:vendor555"

	if _, err := v.DepositBond(payerDID, 100.0); err != nil {
		t.Fatalf("DepositBond failed: %v", err)
	}

	lock, err := v.LockBond("apt-100", payerDID, "caller", vendorDID, 100.0)
	if err != nil {
		t.Fatalf("LockBond failed: %v", err)
	}

	// Release non-existent lock.
	if _, err := v.ReleaseBond(""); err != escrow.ErrLockNotFound {
		t.Fatalf("expected ErrLockNotFound for empty lockID, got %v", err)
	}
	if _, err := v.ReleaseBond("non-existent-lock"); err != escrow.ErrLockNotFound {
		t.Fatalf("expected ErrLockNotFound, got %v", err)
	}

	receipt, err := v.ReleaseBond(lock.LockID)
	if err != nil {
		t.Fatalf("ReleaseBond failed: %v", err)
	}

	// 100 * 0.005 = 0.50 fee; 100 - 0.50 = 99.50 released.
	if receipt.NetworkIntegrityFeeUSD != 0.50 {
		t.Fatalf("expected 0.50 NetworkIntegrityFeeUSD, got %f", receipt.NetworkIntegrityFeeUSD)
	}
	if receipt.ReleasedToUserUSD != 99.50 {
		t.Fatalf("expected 99.50 ReleasedToUserUSD, got %f", receipt.ReleasedToUserUSD)
	}
	if receipt.Status != escrow.LockStatusReleased {
		t.Fatalf("expected LockStatusReleased, got %s", receipt.Status)
	}

	// Check payer account balances.
	payer, err := v.GetAccount(payerDID)
	if err != nil {
		t.Fatalf("GetAccount failed: %v", err)
	}
	if payer.AvailableBondUSD != 99.50 {
		t.Fatalf("expected AvailableBondUSD 99.50, got %f", payer.AvailableBondUSD)
	}
	if payer.LockedBondUSD != 0.0 {
		t.Fatalf("expected LockedBondUSD 0.0, got %f", payer.LockedBondUSD)
	}
	if payer.SettledTxCount != 1 {
		t.Fatalf("expected SettledTxCount 1, got %d", payer.SettledTxCount)
	}
	if payer.CumulativeSpendUSD != 100.0 {
		t.Fatalf("expected CumulativeSpendUSD 100.0, got %f", payer.CumulativeSpendUSD)
	}

	// Check vault stats.
	totLocked, totAvail, treasuryFees, totSlashed := v.Stats()
	if totLocked != 0.0 || totAvail != 99.50 || treasuryFees != 0.50 || totSlashed != 0.0 {
		t.Fatalf("Stats mismatch: locked=%f avail=%f treasury=%f slashed=%f", totLocked, totAvail, treasuryFees, totSlashed)
	}

	// Double-release prevention.
	if _, err := v.ReleaseBond(lock.LockID); err != escrow.ErrLockNotActive {
		t.Fatalf("expected ErrLockNotActive on double release, got %v", err)
	}
	// Double-slash prevention on already released lock.
	if _, err := v.SlashBond(lock.LockID, 0.0); err != escrow.ErrLockNotActive {
		t.Fatalf("expected ErrLockNotActive on slash of released lock, got %v", err)
	}
}

// TestSlashBondDistribution tests the 70%/30% vendor/treasury split and extra penalty deduction.
//
// Pre-conditions: active EscrowLock with locked collateral and payer having available bond.
// Post-conditions: 70% compensated to vendor, 30% retained by treasury, extra penalty deducted.
func TestSlashBondDistribution(t *testing.T) {
	v := escrow.NewVault()
	payerDID := "did:key:payerSlash1"
	vendorDID := "did:key:vendorSlash1"

	// Deposit $200. Lock $100. Available remaining = $100.
	if _, err := v.DepositBond(payerDID, 200.0); err != nil {
		t.Fatalf("DepositBond failed: %v", err)
	}

	lock, err := v.LockBond("apt-slash-01", payerDID, "caller", vendorDID, 100.0)
	if err != nil {
		t.Fatalf("LockBond failed: %v", err)
	}

	// Validation on slash arguments.
	if _, err := v.SlashBond("", 0.0); err != escrow.ErrLockNotFound {
		t.Fatalf("expected ErrLockNotFound for empty lockID, got %v", err)
	}
	if _, err := v.SlashBond("non-existent-lock", 0.0); err != escrow.ErrLockNotFound {
		t.Fatalf("expected ErrLockNotFound for non-existent lock, got %v", err)
	}
	if _, err := v.SlashBond(lock.LockID, -10.0); err != escrow.ErrNegativeExtraPenalty {
		t.Fatalf("expected ErrNegativeExtraPenalty, got %v", err)
	}

	// Slash with extra penalty $50.
	// Total pool = 100 (locked) + 50 (extra deducted from $100 available) = $150.
	// Vendor 70% = 150 * 0.70 = 105.00
	// Treasury 30% = 150 - 105.00 = 45.00
	receipt, err := v.SlashBond(lock.LockID, 50.0)
	if err != nil {
		t.Fatalf("SlashBond failed: %v", err)
	}

	if receipt.Status != escrow.LockStatusSlashed {
		t.Fatalf("expected LockStatusSlashed, got %s", receipt.Status)
	}
	if receipt.VendorCompensationUSD != 105.00 {
		t.Fatalf("expected VendorCompensationUSD 105.00, got %f", receipt.VendorCompensationUSD)
	}
	if receipt.NetworkArbitrationFeeUSD != 45.00 {
		t.Fatalf("expected NetworkArbitrationFeeUSD 45.00, got %f", receipt.NetworkArbitrationFeeUSD)
	}
	if receipt.GoodBehaviorBondDeductedUSD != 50.00 {
		t.Fatalf("expected GoodBehaviorBondDeductedUSD 50.00, got %f", receipt.GoodBehaviorBondDeductedUSD)
	}

	// Verify payer account.
	payer, err := v.GetAccount(payerDID)
	if err != nil {
		t.Fatalf("GetAccount failed: %v", err)
	}
	if payer.AvailableBondUSD != 50.00 {
		t.Fatalf("expected AvailableBondUSD 50.00, got %f", payer.AvailableBondUSD)
	}
	if payer.LockedBondUSD != 0.0 {
		t.Fatalf("expected LockedBondUSD 0.0, got %f", payer.LockedBondUSD)
	}
	if payer.TotalSlashedUSD != 150.00 {
		t.Fatalf("expected TotalSlashedUSD 150.00, got %f", payer.TotalSlashedUSD)
	}

	// Verify vendor account received compensation.
	vendor, err := v.GetAccount(vendorDID)
	if err != nil {
		t.Fatalf("GetAccount for vendor failed: %v", err)
	}
	if vendor.VendorRewardsUSD != 105.00 {
		t.Fatalf("expected vendor VendorRewardsUSD 105.00, got %f", vendor.VendorRewardsUSD)
	}
	if vendor.AvailableBondUSD != 105.00 {
		t.Fatalf("expected vendor AvailableBondUSD 105.00, got %f", vendor.AvailableBondUSD)
	}

	// Verify Stats.
	totLocked, totAvail, treasuryFees, totSlashed := v.Stats()
	if totLocked != 0.0 {
		t.Fatalf("expected totalLocked 0.0, got %f", totLocked)
	}
	// Available: payer (50.00) + vendor (105.00) = 155.00.
	if totAvail != 155.00 {
		t.Fatalf("expected totalAvailable 155.00, got %f", totAvail)
	}
	if treasuryFees != 45.00 {
		t.Fatalf("expected treasuryFees 45.00, got %f", treasuryFees)
	}
	if totSlashed != 150.00 {
		t.Fatalf("expected totalSlashed 150.00, got %f", totSlashed)
	}

	// Double-slash prevention.
	if _, err := v.SlashBond(lock.LockID, 0.0); err != escrow.ErrLockNotActive {
		t.Fatalf("expected ErrLockNotActive on double slash, got %v", err)
	}
	// Release of slashed lock prevention.
	if _, err := v.ReleaseBond(lock.LockID); err != escrow.ErrLockNotActive {
		t.Fatalf("expected ErrLockNotActive on release of slashed lock, got %v", err)
	}
}

// TestSlashBondExceedsAvailable tests that extra penalty deduction is capped at available bond.
//
// Pre-conditions: active EscrowLock where requested penalty exceeds AvailableBondUSD.
// Post-conditions: all available bond is deducted, AvailableBondUSD becomes 0, remainder is not negative.
func TestSlashBondExceedsAvailable(t *testing.T) {
	v := escrow.NewVault()
	payerDID := "did:key:payerCap1"
	vendorDID := "did:key:vendorCap1"

	if _, err := v.DepositBond(payerDID, 100.0); err != nil {
		t.Fatalf("DepositBond failed: %v", err)
	}
	// Lock $80, Available remaining = $20.
	lock, err := v.LockBond("apt-cap-01", payerDID, "caller", vendorDID, 80.0)
	if err != nil {
		t.Fatalf("LockBond failed: %v", err)
	}

	// Slash with extra penalty $50 (more than available $20).
	receipt, err := v.SlashBond(lock.LockID, 50.0)
	if err != nil {
		t.Fatalf("SlashBond failed: %v", err)
	}

	if receipt.GoodBehaviorBondDeductedUSD != 20.00 {
		t.Fatalf("expected GoodBehaviorBondDeductedUSD 20.00, got %f", receipt.GoodBehaviorBondDeductedUSD)
	}

	// Total pool = 80 + 20 = 100.
	// Vendor 70% = 70.00.
	// Treasury 30% = 30.00.
	if receipt.VendorCompensationUSD != 70.00 {
		t.Fatalf("expected VendorCompensationUSD 70.00, got %f", receipt.VendorCompensationUSD)
	}
	if receipt.NetworkArbitrationFeeUSD != 30.00 {
		t.Fatalf("expected NetworkArbitrationFeeUSD 30.00, got %f", receipt.NetworkArbitrationFeeUSD)
	}

	payer, err := v.GetAccount(payerDID)
	if err != nil {
		t.Fatalf("GetAccount failed: %v", err)
	}
	if payer.AvailableBondUSD != 0.0 {
		t.Fatalf("expected AvailableBondUSD 0.0, got %f", payer.AvailableBondUSD)
	}
}

// TestListLocksAndGetLock tests lock retrieval and listing sorting.
//
// Pre-conditions: multiple active locks created across appointments.
// Post-conditions: ListLocks returns all locks ordered by CreatedAt ascending.
func TestListLocksAndGetLock(t *testing.T) {
	v := escrow.NewVault()
	payerDID := "did:key:listPayer"
	vendorDID := "did:key:listVendor"

	if _, err := v.DepositBond(payerDID, 500.0); err != nil {
		t.Fatalf("DepositBond failed: %v", err)
	}

	lock1, err := v.LockBond("apt-1", payerDID, "c1", vendorDID, 50.0)
	if err != nil {
		t.Fatalf("LockBond 1 failed: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	lock2, err := v.LockBond("apt-2", payerDID, "c2", vendorDID, 75.0)
	if err != nil {
		t.Fatalf("LockBond 2 failed: %v", err)
	}

	// GetLock validation.
	if _, err := v.GetLock(""); err != escrow.ErrLockNotFound {
		t.Fatalf("expected ErrLockNotFound for empty lockID, got %v", err)
	}
	if _, err := v.GetLock("unknown-id"); err != escrow.ErrLockNotFound {
		t.Fatalf("expected ErrLockNotFound for unknown lockID, got %v", err)
	}

	fetched1, err := v.GetLock(lock1.LockID)
	if err != nil {
		t.Fatalf("GetLock 1 failed: %v", err)
	}
	if fetched1.AppointmentID != "apt-1" || fetched1.AmountUSD != 50.0 {
		t.Fatalf("fetched lock 1 mismatch: %+v", fetched1)
	}

	locks := v.ListLocks()
	if len(locks) != 2 {
		t.Fatalf("expected 2 locks, got %d", len(locks))
	}
	if locks[0].LockID != lock1.LockID || locks[1].LockID != lock2.LockID {
		t.Fatalf("ListLocks ordering mismatch: [%s, %s]", locks[0].LockID, locks[1].LockID)
	}
}

// TestConcurrentOperationsStress tests thread safety and race detector across 32 goroutines.
//
// Pre-conditions: 32 concurrent goroutines performing concurrent deposits, locks, releases, and slashes.
// Post-conditions: no data race or panic occurs and internal accounting remains consistent.
func TestConcurrentOperationsStress(t *testing.T) {
	const goroutineCount = 32
	v := escrow.NewVault()

	// Initial funding for multiple test agents.
	for i := 0; i < goroutineCount; i++ {
		did := fmt.Sprintf("did:key:agent-%d", i)
		if _, err := v.DepositBond(did, 1000.0); err != nil {
			t.Fatalf("DepositBond failed for agent %d: %v", i, err)
		}
	}

	var wg sync.WaitGroup
	wg.Add(goroutineCount)

	for i := 0; i < goroutineCount; i++ {
		workerID := i
		go func() {
			defer wg.Done()

			payerDID := fmt.Sprintf("did:key:agent-%d", workerID)
			vendorDID := fmt.Sprintf("did:key:vendor-%d", (workerID+1)%goroutineCount)
			aptID := fmt.Sprintf("apt-stress-%d", workerID)

			// Record stripe spend
			if _, err := v.RecordStripeSpend(payerDID, 10.0, 1); err != nil {
				t.Errorf("RecordStripeSpend error: %v", err)
				return
			}

			// Lock bond
			lock, err := v.LockBond(aptID, payerDID, "caller", vendorDID, 50.0)
			if err != nil {
				t.Errorf("LockBond error: %v", err)
				return
			}

			// Alternating release vs slash
			if workerID%2 == 0 {
				if _, err := v.ReleaseBond(lock.LockID); err != nil {
					t.Errorf("ReleaseBond error: %v", err)
				}
			} else {
				if _, err := v.SlashBond(lock.LockID, 10.0); err != nil {
					t.Errorf("SlashBond error: %v", err)
				}
			}

			// Read operations under concurrent activity
			_ = v.ListLocks()
			_, _ = v.GetAccount(payerDID)
			_, _ = v.GetLock(lock.LockID)
			_, _, _, _ = v.Stats()
		}()
	}

	wg.Wait()

	totLocked, totAvail, treasuryFees, totSlashed := v.Stats()
	if totLocked != 0.0 {
		t.Fatalf("expected total locked USD to resolve to 0.0, got %f", totLocked)
	}
	if totAvail <= 0.0 {
		t.Fatalf("expected total available USD to be positive, got %f", totAvail)
	}
	if treasuryFees <= 0.0 {
		t.Fatalf("expected positive treasury fees from release and slash, got %f", treasuryFees)
	}
	if totSlashed <= 0.0 {
		t.Fatalf("expected positive total slashed funds, got %f", totSlashed)
	}
}
