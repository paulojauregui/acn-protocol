# Agentic Communication Network (ACN) — Engineering Roadmap

This document tracks the sequential delivery of `paulojauregui/acn-protocol` across 18 single-responsibility milestones in Go `1.27.4`, organized into two iterations:

1. **Iteration 1 (`v1-mvp`)**: Sovereign identity, reputation, blind escrow, accountability oracle, Merkle ledger, governance, relay, simulator, control-plane API, embedded console, container, and GitOps delivery (`M01` through `M12`, shipped in Pull Request #1).
2. **Iteration 2 (`v2-ux`)**: Embedded console modularization, design-system tokens, semantic HTML5 shell, modular JavaScript renderers and controller, and documentation/build verification (`M13` through `M18`, shipped in Pull Request #2).

---

## Iteration 1 (`v1-mvp`): Core Protocol, Control Plane, Container & GitOps Delivery

### Milestone Matrix

| Milestone | Domain / Package | Deliverables | Verification Gate |
| :--- | :--- | :--- | :--- |
| **M01** | PM, ADR, Protobuf & Module Bootstrap | `docs/PRD.md`, `docs/adr/0001-go-acn-protocol-and-accountability-oracle.md`, `ROADMAP.md`, `docs/specs/M01_SPEC.md`, `proto/acn/v1/acn.proto`, `go.mod` | `go mod verify` |
| **M02** | Sovereign Identity, DIDs & Rotating Sub-Agents | `docs/specs/M02_SPEC.md`, `internal/identity/did.go`, `internal/identity/registry.go`, `internal/identity/registry_test.go` | `go test -v -race -cover ./internal/identity/...` |
| **M03** | Sybil-Resistant & Whale-Resistant Reputation Engine | `docs/specs/M03_SPEC.md`, `internal/reputation/math.go`, `internal/reputation/engine.go`, `internal/reputation/engine_test.go` | `go test -v -race -cover ./internal/reputation/...` |
| **M04** | Stripe ACS Proof-of-Capital & Blind Escrow Vault | `docs/specs/M04_SPEC.md`, `internal/escrow/bond.go`, `internal/escrow/vault.go`, `internal/escrow/vault_test.go` | `go test -v -race -cover ./internal/escrow/...` |
| **M05** | Double-Blind Accountability Oracle & 60s Nonce Handshake | `docs/specs/M05_SPEC.md`, `internal/oracle/handshake.go`, `internal/oracle/oracle.go`, `internal/oracle/oracle_test.go` | `go test -v -race -cover ./internal/oracle/...` |
| **M06** | Merkle Tree Ledger Batcher & Privacy-Preserving Trust Passports | `docs/specs/M06_SPEC.md`, `internal/ledger/merkle.go`, `internal/ledger/passport.go`, `internal/ledger/ledger_test.go` | `go test -v -race -cover ./internal/ledger/...` |
| **M07** | Governance Constitution, Reputation-Weighted Voting & DGX Spark Jury | `docs/specs/M07_SPEC.md`, `internal/governance/voting.go`, `internal/governance/jury.go`, `internal/governance/governance_test.go` | `go test -v -race -cover ./internal/governance/...` |
| **M08** | High-Concurrency Agent Relay & SIP-Style Handshake Router | `docs/specs/M08_SPEC.md`, `internal/relay/router.go`, `internal/relay/router_test.go` | `go test -v -race -cover ./internal/relay/...` |
| **M09** | Sybil Swarm, Whale Attack & Collusive Vendor Simulator | `docs/specs/M09_SPEC.md`, `internal/simulator/scenarios.go`, `internal/simulator/scenarios_test.go` | `go test -v -race -cover ./internal/simulator/...` |
| **M10** | HTTP/2 REST & Connect-Style RPC Control-Plane API Server | `docs/specs/M10_SPEC.md`, `internal/api/server.go`, `internal/api/handlers.go`, `internal/api/server_test.go` | `go test -v -race -cover ./internal/api/...` |
| **M11** | Embedded Interactive ACN RaaS & Attack Simulator Console + Entrypoint | `docs/specs/M11_SPEC.md`, `internal/api/ui/index.html`, `cmd/acn-node/main.go`, `cmd/acn-node/main_test.go` | `go test -v -race -cover ./...` |
| **M12** | Hardened Container, Kustomize GitOps Manifests, QA & PR #1 | `docs/specs/M12_SPEC.md`, `Dockerfile`, `deploy/base/*`, `README.md`, Pull Request #1 | `kubectl kustomize deploy/base` & CI checks |

---

## Iteration 2 (`v2-ux`): UI/UX, Accessibility & Embedded Console Modularization

### Milestone Matrix

| Milestone | Domain / Package | Deliverables | Verification Gate |
| :--- | :--- | :--- | :--- |
| **M13** | Design System Tokens & Component Stylesheets | `docs/specs/M13_SPEC.md`, `internal/api/ui/tokens.css`, `internal/api/ui/components.css`, `internal/api/ui_assets_test.go` | `go test -v -race ./internal/api/...` |
| **M14** | Go `embed.FS` Multi-File Asset Serving | `docs/specs/M14_SPEC.md`, `internal/api/server.go`, `internal/api/handlers.go`, `internal/api/ui_assets_test.go` | `go test -v -race ./internal/api/...` |
| **M15** | Semantic HTML5 5-Tab Console Shell | `docs/specs/M15_SPEC.md`, `internal/api/ui/index.html`, `internal/api/ui_html_test.go` | `go test -v -race ./internal/api/...` |
| **M16** | Modular JS Core View Renderers & 5-State Skeletons | `docs/specs/M16_SPEC.md`, `internal/api/ui/views.js`, `internal/api/ui_test.go` | `go test -v -race ./internal/api/...` |
| **M17** | Modular JS Application Controller, Keyboard Navigation & API Client | `docs/specs/M17_SPEC.md`, `internal/api/ui/app.js`, `internal/api/ui_test.go` | `go test -v -race ./internal/api/...` |
| **M18** | Binary Build, Kustomize & Documentation Verification | `docs/specs/M18_SPEC.md`, `ROADMAP.md`, `README.md`, Pull Request #2 | `CGO_ENABLED=0 go build ./cmd/acn-node` & `kubectl kustomize deploy/base` & CI checks |

Reference documents for Iteration 2: `docs/UX_SPEC.md` (interaction architecture, information architecture, keyboard and 5-state contracts) and `docs/DESIGN_SYSTEM.md` (design-token contract, 4-tier surface elevation, typography, and component specifications).

---

## Architectural & Quality Invariants Across All Milestones

1. **Design by Contract (DbC)**: Every exported Go type, interface, function, and method documents explicit `Pre-conditions:` and `Post-conditions:`.
2. **Zero External C Dependencies**: Pure Go `1.27.4` (`CGO_ENABLED=0`) using standard library cryptography and concurrency primitives.
3. **Concurrency Safety**: All stateful registries, vaults, ledgers, and routers use `sync.RWMutex` and are verified under `go test -race`.
4. **Zero Emojis**: Strictly professional technical documentation and source code without emojis.
5. **Single Embedding Surface** (`v2-ux`): All console assets live under `internal/api/ui/` and are embedded once by the `//go:embed ui/*` directive in `internal/api/server.go`; `index.html` carries no inline `<style>` or `<script>` blocks and never bypasses the `GET /{$}` and `GET /ui/{asset...}` handlers.
6. **Keyboard Operability & WCAG 2.2 AA** (`v2-ux`): Every interactive console control is reachable and operable by keyboard, the tab list implements the WAI-ARIA roving `tabindex` pattern, and every form control exposes a programmatic label.
7. **Deterministic 5-State Feedback** (`v2-ux`): Every operational surface renders `Empty`, `Loading` (fixed-geometry `CLS = 0` skeletons), `Nominal`, `Degraded`, and `Error` states without `window.alert()`.
