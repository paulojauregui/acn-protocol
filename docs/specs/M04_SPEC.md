# Milestone M04 Specification: Stripe ACS Proof-of-Capital & Blind Escrow Vault (`internal/escrow`)

## 1. Objective
Implement Good-Behavior Bond accounts, Stripe ACS Proof-of-Spend ledger tracking (`CumulativeSpendUSD`, `SettledTxCount`), per-appointment Blind Escrow Locks (`LockBond`, `ReleaseBond`, `SlashBond`), and the `0.5%` Network Integrity Fee + `70%/30%` Vendor/Network Slashing distribution in package `escrow` (`github.com/paulojauregui/acn-protocol/internal/escrow`).

## 2. Target Files
1. `internal/escrow/bond.go`
2. `internal/escrow/vault.go`
3. `internal/escrow/vault_test.go`

## 3. Design-by-Contract & Architectural Invariants
- **Every exported struct, interface, constant, function, and method MUST have a Go doc comment (`// SymbolName ...`) that explicitly states `Pre-conditions:` and `Post-conditions:`.**
- **Zero emojis** in code or comments.
- Keep implementation concise and clean using only the Go 1.27.4 standard library (`crypto/sha256`, `encoding/hex`, `errors`, `fmt`, `math`, `sort`, `sync`, `time`).

## 4. Exact Signatures & Economics

### 4.1 `internal/escrow/bond.go`
- Constants:
  - `NetworkIntegrityFeeRate = 0.005` (`0.5%` fee on successful settlement)
  - `VendorSlashCompensationRate = 0.70` (`70%` of slashed bond paid to aggrieved Vendor)
  - `NetworkArbitrationFeeRate = 0.30` (`30%` of slashed bond retained by ACN Treasury)
- `type LockStatus string`
  - `LockStatusActive LockStatus = "LOCKED"`
  - `LockStatusReleased LockStatus = "RELEASED"`
  - `LockStatusSlashed LockStatus = "SLASHED"`
- `type BondAccount struct`:
  - `RootDID string` (`json:"root_did"`)
  - `AvailableBondUSD float64` (`json:"available_bond_usd"`)
  - `LockedBondUSD float64` (`json:"locked_bond_usd"`)
  - `CumulativeSpendUSD float64` (`json:"cumulative_spend_usd"`)
  - `SettledTxCount int` (`json:"settled_tx_count"`)
  - `TotalSlashedUSD float64` (`json:"total_slashed_usd"`)
  - `VendorRewardsUSD float64` (`json:"vendor_rewards_usd"`)
  - `UpdatedAt time.Time` (`json:"updated_at"`)
- `type EscrowLock struct`:
  - `LockID string` (`json:"lock_id"`)
  - `AppointmentID string` (`json:"appointment_id"`)
  - `PayerRootDID string` (`json:"payer_root_did"`)
  - `CallerDID string` (`json:"caller_did"`)
  - `VendorDID string` (`json:"vendor_did"`)
  - `AmountUSD float64` (`json:"amount_usd"`)
  - `Status LockStatus` (`json:"status"`)
  - `NetworkFeeUSD float64` (`json:"network_fee_usd"`)
  - `VendorPayoutUSD float64` (`json:"vendor_payout_usd"`)
  - `CreatedAt time.Time` (`json:"created_at"`)
  - `SettledAt time.Time` (`json:"settled_at"`)
- `type SettlementReceipt struct`:
  - `LockID string` (`json:"lock_id"`)
  - `AppointmentID string` (`json:"appointment_id"`)
  - `Status LockStatus` (`json:"status"`)
  - `ReleasedToUserUSD float64` (`json:"released_to_user_usd"`)
  - `NetworkIntegrityFeeUSD float64` (`json:"network_integrity_fee_usd"`)
  - `VendorCompensationUSD float64` (`json:"vendor_compensation_usd"`)
  - `NetworkArbitrationFeeUSD float64` (`json:"network_arbitration_fee_usd"`)
  - `GoodBehaviorBondDeductedUSD float64` (`json:"good_behavior_bond_deducted_usd"`)
- `func RoundCents(amount float64) float64`
  - Returns `math.Round(amount*100.0) / 100.0`.

### 4.2 `internal/escrow/vault.go`
- `type Vault struct` with `sync.RWMutex`, tracking `accounts map[string]*BondAccount`, `locks map[string]*EscrowLock`, `treasuryFeesUSD float64`, and `totalSlashedUSD float64`.
- `func NewVault() *Vault`
- `func (v *Vault) DepositBond(rootDID string, amountUSD float64) (*BondAccount, error)`
  - Validates `rootDID != ""` and `amountUSD > 0`. Adds `amountUSD` to `AvailableBondUSD`.
- `func (v *Vault) RecordStripeSpend(rootDID string, spendUSD float64, txCountDelta int) (*BondAccount, error)`
  - Validates `rootDID != ""`, `spendUSD >= 0`, `txCountDelta >= 0`. Updates `CumulativeSpendUSD` and `SettledTxCount`.
- `func (v *Vault) GetAccount(rootDID string) (*BondAccount, error)`
- `func (v *Vault) LockBond(appointmentID, payerRootDID, callerDID, vendorDID string, amountUSD float64) (*EscrowLock, error)`
  - Validates `appointmentID != ""`, `payerRootDID != ""`, `vendorDID != ""`, `amountUSD > 0`.
  - Requires `account.AvailableBondUSD >= amountUSD`. Moves `amountUSD` from `AvailableBondUSD` to `LockedBondUSD` and creates an `EscrowLock` with `Status: LockStatusActive`.
- `func (v *Vault) ReleaseBond(lockID string) (*SettlementReceipt, error)`
  - Requires lock in `LockStatusActive`.
  - Calculates `fee = RoundCents(lock.AmountUSD * NetworkIntegrityFeeRate)` (`0.5%`), `released = RoundCents(lock.AmountUSD - fee)`.
  - Decrements `LockedBondUSD` by `lock.AmountUSD`, adds `released` back to `AvailableBondUSD`, increments `SettledTxCount` by `1`, adds `lock.AmountUSD` to `CumulativeSpendUSD`, adds `fee` to `treasuryFeesUSD`, and marks lock `LockStatusReleased`.
- `func (v *Vault) SlashBond(lockID string, extraPenaltyUSD float64) (*SettlementReceipt, error)`
  - Requires lock in `LockStatusActive`.
  - Slashes `100%` of `lock.AmountUSD` from `LockedBondUSD`. If `extraPenaltyUSD > 0`, also deducts up to `extraPenaltyUSD` from the payer's `AvailableBondUSD` (`GoodBehaviorBondDeductedUSD`).
  - Splits total slashed pool (`totalPool = lock.AmountUSD + deductedExtra`): `VendorCompensationUSD = RoundCents(totalPool * VendorSlashCompensationRate)` (`70%`) credited to vendor's `BondAccount.VendorRewardsUSD` and `AvailableBondUSD`, and `NetworkArbitrationFeeUSD = RoundCents(totalPool - VendorCompensationUSD)` (`30%`) credited to `treasuryFeesUSD`.
  - Marks lock `LockStatusSlashed`.
- `func (v *Vault) GetLock(lockID string) (*EscrowLock, error)`
- `func (v *Vault) ListLocks() []*EscrowLock`
- `func (v *Vault) Stats() (totalLockedUSD, totalAvailableUSD, treasuryFeesUSD, totalSlashedUSD float64)`

### 4.3 `internal/escrow/vault_test.go`
- Unit tests verifying:
  - `DepositBond`, `RecordStripeSpend`, and insufficient-funds rejection on `LockBond`.
  - `ReleaseBond` `0.5%` Network Integrity Fee calculation and `99.5%` return to `AvailableBondUSD`.
  - `SlashBond` `70%` Vendor compensation + `30%` Network Arbitration Fee split, plus deduction from Root Agent's `AvailableBondUSD` when `extraPenaltyUSD > 0`.
  - Double-settlement/double-slash prevention and `-race` safety across 32 goroutines.

## 5. Verification Command
```bash
export PATH="/home/tron/.local/go/bin:$PATH" && test -z "$(gofmt -l internal/escrow)" && go vet ./internal/escrow/... && go test -v -race -cover ./internal/escrow/...
```
