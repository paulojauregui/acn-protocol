# Milestone M09 Specification: Sybil Swarm, Whale Attack & Collusive Vendor Simulator (`internal/simulator`)

## 1. Objective
Implement deterministic adversarial simulations proving the mathematical soundness of the ACN Protocol against three primary threat models in package `simulator` (`github.com/paulojauregui/acn-protocol/internal/simulator`):
1. `RunSybilSwarmSimulation`: 1,000 zero-stake/low-stake Sybil bots wash-trading $0 pings vs. 10 bonded honest agents.
2. `RunWhaleAttackSimulation`: Brand-new account with $1,000,000 capital attempting 500 concurrent high-value bookings vs. Time-Weighted Velocity ($\nu$).
3. `RunCollusiveVendorSimulation`: Malicious vendor fabricating a No-Show claim against an honest user armed with a 60s challenge nonce + Stripe POS receipt + hardware beacon.

## 2. Target Files
1. `internal/simulator/scenarios.go`
2. `internal/simulator/scenarios_test.go`

## 3. Design-by-Contract & Architectural Invariants
- **Every exported struct, interface, constant, function, and method MUST have a Go doc comment (`// SymbolName ...`) that explicitly states `Pre-conditions:` and `Post-conditions:`.**
- **Zero emojis** in code or comments.
- Simulations must execute deterministically in `< 50ms` so unit tests and API triggers respond instantaneously.

## 4. Exact Signatures & Scenarios

### 4.1 `internal/simulator/scenarios.go`
- `type SimulationResult struct`:
  - `Scenario string` (`json:"scenario"`)
  - `DefenseSucceeded bool` (`json:"defense_succeeded"`)
  - `Summary string` (`json:"summary"`)
  - `Metrics map[string]any` (`json:"metrics"`)
  - `DurationMs float64` (`json:"duration_ms"`)
  - `ExecutedAt time.Time` (`json:"executed_at"`)
- `func RunSybilSwarmSimulation() *SimulationResult`
  - Spawns 1,000 Sybil bots (`$0` bond, wash-trading `$0` endorsements among each other) and 10 Honest Bonded Agents (`$250` bond, `$1,200` verified Stripe spend across 25 settled bookings, endorsed by staked peers).
  - Verifies that Weight-of-Stake ($\omega$) keeps all 1,000 Sybil bots at or below their baseline score (`<= 2.5`, Tier 0 Ephemeral, `0` high-value bookings allowed), while the 10 Honest Agents reach `score >= 4.2` and `Tier 2 Reputational` / `Tier 3 Sovereign`.
  - Populates `Metrics` with `sybil_bot_count: 1000`, `honest_agent_count: 10`, `sybil_avg_score`, `honest_avg_score`, `sybil_tier_0_trapped_pct: 100.0`, `sybil_high_value_bookings_approved: 0`.
- `func RunWhaleAttackSimulation() *SimulationResult`
  - Simulates a brand-new Whale agent (`StakeUSD: 1000000.0`, `AccountAgeDays: 0.0`, `SettledTxCount: 0`) attempting 500 concurrent `$100` autonomous vehicle bookings (`$50,000` total exposure).
  - Computes initial `VelocityCapacity = ComputeVelocityCapacity(1000000.0, 0.0, 0, 25.0)` (`0.0` at `t=0`, or `<= $150` for a 0.0001-day-old account), proving that 498+ of the 500 concurrent high-value bookings are throttled until longitudinal age and settled transaction count accrue.
  - Populates `Metrics` with `whale_deposit_usd: 1000000.0`, `attempted_concurrent_bookings: 500`, `approved_bookings_at_t0`, `blocked_bookings_at_t0`, `velocity_capacity_t0_usd`, `velocity_capacity_day30_usd`.
- `func RunCollusiveVendorSimulation() *SimulationResult`
  - Simulates an honest user booking a `$100` appointment, completing the 60s venue challenge handshake (`UserPresent: true`, `StripePOSReceipt: true`, `LocalBeaconVerified: true`), while a malicious vendor submits `VendorPresent: false` (attempting to steal the `70%` slash payout).
  - Runs `oracle.SettleAppointment` and `governance.Jury.ArbitrateDispute`: proves the Oracle flags `STATUS_DISPUTED`, the Jury rules `Winner: "USER"`, `SlashUser: false`, `SlashVendor: true`, releases the user's escrow bond intact, and slashes the vendor's Merchant Reputation.
  - Populates `Metrics` with `oracle_status`, `jury_winner`, `user_bond_slashed: false`, `vendor_slashed: true`, `vendor_reputation_before`, `vendor_reputation_after`.

### 4.2 `internal/simulator/scenarios_test.go`
- Unit tests running all three scenarios (`RunSybilSwarmSimulation`, `RunWhaleAttackSimulation`, `RunCollusiveVendorSimulation`) and asserting `DefenseSucceeded == true` and all expected invariants.

## 5. Verification Command
```bash
export PATH="/home/tron/.local/go/bin:$PATH" && test -z "$(gofmt -l internal/simulator)" && go vet ./internal/simulator/... && go test -v -race -cover ./internal/simulator/...
```
