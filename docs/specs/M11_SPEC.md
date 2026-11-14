# Milestone M11 Specification: Embedded Interactive ACN RaaS & Attack Simulator Web Console + `cmd/acn-node`

## 1. Objective
Implement the embedded 5-tab interactive engineering Web Console (`internal/api/ui/index.html`) served via Go `//go:embed` at `GET /`, and the production HTTP server entrypoint (`cmd/acn-node/main.go` and `cmd/acn-node/main_test.go`).

## 2. Target Files
1. `internal/api/ui/index.html`
2. `cmd/acn-node/main.go`
3. `cmd/acn-node/main_test.go`

## 3. Design-by-Contract & Architectural Invariants
- **Every exported struct, interface, constant, function, and method MUST have a Go doc comment (`// SymbolName ...`) that explicitly states `Pre-conditions:` and `Post-conditions:`.**
- **Zero emojis** in `index.html`, `main.go`, or `main_test.go`.
- `internal/api/ui/index.html` must present 5 interactive tabs backed by live `fetch()` calls to `/api/v1/*`:
  1. **Network Overview & Sovereign DID Directory** (Root DIDs, Rotating Sub-Agents, 4-Tier Trust Ladder, Bond & Spend stats, Register Root / Spawn Rotating Sub-Agent forms)
  2. **Live Agent Relay & Blind Escrow Negotiator** (Interactive caller-to-vendor booking flow with live state machine visualization: `INITIATED -> REPUTATION_VERIFIED -> ESCROW_LOCKED -> CONFIRMED | REJECTED_LOW_TRUST`)
  3. **Accountability Oracle & Double-Blind Check-In** (Issue 60s nonce, test Mutual Arrival `STATUS_SUCCESS`, No-Show Slashing `STATUS_SLASHED`, or Conflicting Dispute Arbitration `STATUS_DISPUTED`)
  4. **RaaS Privacy Passport & Merkle Ledger Explorer** (Generate selective-disclosure Trust Passports, verify signatures, inspect SHA-256 Merkle root and inclusion proofs)
  5. **Adversarial Threat Model Lab** (One-click interactive runners for *1,000-Bot Sybil Swarm*, *\$1M Whale Attack*, and *Collusive Vendor Audit* with comparative metrics)

## 4. `cmd/acn-node/main.go`
- Reads `PORT` environment variable (default `"8000"`).
- Starts HTTP server with `ReadHeaderTimeout: 5 * time.Second`, `ReadTimeout: 15 * time.Second`, `WriteTimeout: 15 * time.Second`, `IdleTimeout: 60 * time.Second`.
- Handles `SIGINT` and `SIGTERM` for graceful shutdown.

## 5. Verification Command
```bash
export PATH="/home/tron/.local/go/bin:$PATH" && test -z "$(gofmt -l .)" && go vet ./... && go test -v -race -cover ./...
```
