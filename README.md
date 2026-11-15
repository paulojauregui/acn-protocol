# Agentic Communication Network (ACN) Protocol

**Sovereign Agent Identity, Sybil & Whale-Resistant Reputation Engine, Stripe ACS Blind Escrow Vault, and Double-Blind Physical Accountability Oracle in Go 1.27.4.**

---

## 1. Overview & Problem Statement

As autonomous AI agents execute physical and commercial commitments—booking autonomous rides, reserving medical screenings, and negotiating restaurant tables—merchants face **Zero-Cost Agentic Denial-of-Inventory**, while human principals face **cross-vendor behavioral tracking** and **unfair slashing** from faulty or collusive vendor check-in systems.

`acn-protocol` resolves this trilemma through four cryptographic and economic layers:

1. **Sovereign Root DIDs & Rotating Unlinkable Sub-Agents (`internal/identity`)**:
   - `RootAgent` (`did:acn:<hex_pubkey_fingerprint>`) backed by `Ed25519` keys, Proof-of-Personhood (`PoPVerified`), and TEE hardware attestation (`TEEAttested`).
   - Ephemeral single-use `SubAgent` addresses (`did:acn:sub:<id>`) prevent vendors from correlating a user's bookings across merchants while rolling all escrow locks, slashes, and reputation updates directly to the parent `RootAgent`.
   - **4-Tier Trust Ladder**:
     - `Tier 0 Ephemeral` (`<= $5.00` max exposure)
     - `Tier 1 Economic` (`<= $150.00` via `>= $50.00` bond or `>= 100` settled Tier-0 completions)
     - `Tier 2 Reputational` (`<= $2,000.00` via `>= $500.00` Stripe Proof-of-Spend across `>= 20` settled transactions + bond)
     - `Tier 3 Sovereign` (Unrestricted exposure with `PoPVerified == true` + composite score `>= 4.50`)
2. **Sybil-Resistant & Whale-Resistant Reputation Engine (`internal/reputation`)**:
   - **4-Component Composite Score (`0.00` to `5.00`)**: Fulfillment Rate (`0.50`), Cancellation Lead-Time (`0.20`), Peer Review Rating (`0.20`), and Stake Seniority (`0.10`).
   - **Weight-of-Stake ($\omega$) Endorsements**, **Time-Weighted Velocity ($\nu$) Whale Defense**, and **Exponential Slash Decay** ("Right to be Forgotten").
3. **Stripe ACS Proof-of-Capital & Blind Escrow Vault (`internal/escrow`)**:
   - `0.5%` Network Integrity Fee on successful settlement (`STATUS_SUCCESS`).
   - Automatic `100%` commitment bond slash on verified `NO_SHOW` (`STATUS_SLASHED`), distributing `70%` to the aggrieved Vendor and `30%` to the ACN Protocol Treasury.
4. **Double-Blind Accountability Oracle, SHA-256 Merkle RaaS Passports & Hybrid AI Jury (`internal/oracle`, `internal/ledger`, `internal/governance`)**:
   - Replay-protected 60-second cryptographic challenge nonces (`IssueChallengeNonce`).
   - Double-Blind Mutual Attestation (`SettleAppointment`) with multi-factor hardware/POS corroboration (`StripePOSReceipt`, `LocalBeaconVerified`) and `< 2.0s` DGX Spark LLM Audit Jury failover (`nvidia/Qwen3.8-Flash-Next-FP4-Local`).
   - Selective-disclosure `TrustPassport` credentials anchored to the SHA-256 `MerkleRoot`.

---

## 2. System Architecture

```mermaid
flowchart TD
    subgraph ClientTier["Autonomous Agents & Operator Web Console"]
        UI["Embedded 5-Tab Web Console<br/>(internal/api/ui/* served via embed.FS)"]
        Sub["Rotating Sub-Agent<br/>(did:acn:sub:*)"]
        Vendor["Merchant Vendor Agent<br/>(Waymo / UCSF / Michelin)"]
    end

    subgraph ControlPlane["ACN Control Plane (cmd/acn-node :8000)"]
        API["HTTP/2 & Connect-RPC Server<br/>(internal/api)"]
        Relay["SIP-Style Handshake Router<br/>(internal/relay)"]
        ID["Sovereign DID Registry<br/>(internal/identity)"]
        Rep["Reputation Engine (omega, nu)<br/>(internal/reputation)"]
        Escrow["Blind Escrow Vault & Bonds<br/>(internal/escrow)"]
        Oracle["Double-Blind Oracle & 60s Nonce<br/>(internal/oracle)"]
        Ledger["SHA-256 Merkle Tree & RaaS Passport<br/>(internal/ledger)"]
        Gov["Constitution & Audit Jury<br/>(internal/governance)"]
        Sim["Adversarial Simulator<br/>(internal/simulator)"]
    end

    subgraph Hardware["On-Premise AI Arbitration Tier"]
        Spark["DGX Spark vLLM (:8000)<br/>nvidia/Qwen3.8-Flash-Next-FP4-Local"]
    end

    UI --> API
    Sub --> API
    Vendor --> API
    API --> Relay
    API --> Oracle
    API --> Ledger
    API --> Gov
    API --> Sim
    Relay --> ID
    Relay --> Rep
    Relay --> Escrow
    Relay --> Ledger
    Oracle --> Escrow
    Oracle --> Rep
    Oracle --> Ledger
    Oracle --> Gov
    Gov -.->|"< 2.0s HTTP Timeout + Deterministic Fallback"| Spark
```

---

## 3. Core Mathematical Formulations

### 3.1 Weight-of-Stake ($\omega$) Endorsement Formula
To prevent Sybil wash-trading where 1,000 unbonded bots endorse each other with $0 pings:

$$T_{\text{new}} = \text{clamp}\left(T_{\text{old}} + \frac{\sum_{i=1}^{N} (R_i \times S_i \times V_i)}{N \times \text{ScaleFactor}}, 0.0, 5.0\right)$$

Where $R_i \in [0.0, 5.0]$ is the endorser's reputation score, $S_i \ge 0$ is the endorser's locked bond in USD, and $V_i \ge 0$ is the settled transaction volume in USD. When $S_i = 0$ or $V_i = 0$, the endorsement delta is strictly `0.0`.

### 3.2 Time-Weighted Velocity ($\nu$) Whale-Attack Defense
To prevent a newly created account with `$1,000,000` in fresh capital (`AccountAgeDays` $\approx 0$) from booking hundreds of high-value slots simultaneously:

$$\text{VelocityCapacity} = (\text{StakeUSD} \times \text{AccountAgeDays}) + (\text{SettledTxCount} \times \text{QualityWeight})$$

### 3.3 Reputation-Weighted Governance Voting ($V_i$)
Constitutional proposals weight each voter by logarithmic stake multiplied by proven reputation:

$$V_i = \ln(1 + S_i) \times R_i$$

---

## 4. Repository Layout

```text
acn-protocol/
├── cmd/acn-node/                # Production HTTP/2 server entrypoint & lifecycle tests
├── deploy/base/                 # Kustomize manifests (Deployment, Service, HTTPRoute)
├── docs/
│   ├── PRD.md                   # Product Requirements Document
│   ├── UX_SPEC.md               # v2-ux interaction architecture, IA & keyboard contracts
│   ├── DESIGN_SYSTEM.md         # v2-ux design tokens, surface elevation & component specs
│   ├── adr/0001-...md           # Architecture Decision Record 0001
│   └── specs/M01..M18_SPEC.md   # 18 Granular Milestone Specifications
├── internal/
│   ├── identity/                # Root DIDs, Ed25519 keys, Rotating Sub-Agents, 4-Tier Ladder
│   ├── reputation/              # Composite score, Weight-of-Stake (omega), Velocity (nu)
│   ├── escrow/                  # Bond accounts, Blind Escrow Locks, 0.5% fee & 70/30 slash
│   ├── oracle/                  # 60s replay-protected nonces & Double-Blind settlement
│   ├── ledger/                  # SHA-256 Merkle tree proofs & selective-disclosure Passports
│   ├── governance/              # Constitution, V_i voting & DGX Spark LLM Audit Jury
│   ├── relay/                   # SIP-style pre-answer handshake router
│   ├── simulator/               # Sybil Swarm, Whale Attack & Collusive Vendor simulations
│   └── api/                     # Control-plane HTTP handlers & embedded Web Console
│       ├── server.go            # //go:embed ui/* embed.FS + route registration
│       ├── handlers.go          # GET /{$} console & GET /ui/{asset...} asset handlers
│       └── ui/                  # Modular embed.FS console assets (no inline CSS/JS)
│           ├── index.html       # Semantic HTML5 shell, ARIA tablist, labeled forms
│           ├── tokens.css       # Design tokens (surfaces, status colors, type, spacing)
│           ├── components.css   # Layout, tab bar, stepper, tables, CLS = 0 skeletons
│           ├── views.js         # Pure DOM renderers, escaping, formatting, 5-state views
│           └── app.js           # Controller, keyboard navigation & /api/v1/* fetch client
├── proto/acn/v1/acn.proto       # Protocol Buffers v3 service & message definitions
├── Dockerfile                   # Multi-stage CGO_ENABLED=0 build (USER 10001:10001)
├── ROADMAP.md                   # 18-Milestone engineering delivery matrix (2 iterations)
└── go.mod                       # Go 1.27.4 module definition
```

### 4.1 Embedded Web Console (`v2-ux`)

The operator console ships inside the `acn-node` binary. `internal/api/server.go` embeds the whole asset directory once with `//go:embed ui/*` into an `embed.FS`, and `internal/api/handlers.go` exposes exactly two serving routes. There are no external CDN dependencies and `index.html` carries no inline `<style>` or `<script>` blocks.

| Embedded Asset | Route | Single Responsibility |
| :--- | :--- | :--- |
| `internal/api/ui/index.html` | `GET /{$}` | Semantic HTML5 landmarks, WAI-ARIA `tablist`/`tab`/`tabpanel` shell for the 5 console views, labeled forms, empty/loading/alert containers |
| `internal/api/ui/tokens.css` | `GET /ui/tokens.css` | Design tokens: 4-tier dark-slate surface palette, semantic status colors, `--font-sans`/`--font-mono` typography, `tabular-nums`, spacing and focus-ring variables |
| `internal/api/ui/components.css` | `GET /ui/components.css` | Responsive layout grids, tab bar, 4-phase relay stepper, badges, data tables, `CLS = 0` skeletons, inline banners |
| `internal/api/ui/views.js` | `GET /ui/views.js` | Pure DOM renderers: HTML escaping, USD/numeric formatting, zero-CLS skeleton generators, table row builders, stepper updater, banner helpers |
| `internal/api/ui/app.js` | `GET /ui/app.js` | Application controller: tab switching, global keyboard shortcuts, 60s nonce countdown timer, `fetch` client for all `/api/v1/*` endpoints |

Authoritative specifications: [docs/UX_SPEC.md](docs/UX_SPEC.md) (interaction architecture, information architecture, keyboard and state contracts) and [docs/DESIGN_SYSTEM.md](docs/DESIGN_SYSTEM.md) (token contract, surface elevation, component specifications).

**Accessibility (WCAG 2.2 AA, keyboard-first)**:

- Tab traversal uses the WAI-ARIA roving `tabindex` pattern on the 5-tab `role="tablist"`: `ArrowRight` / `ArrowLeft` move to the next / previous tab with wraparound, `Home` / `End` jump to the first / last tab. The active tab keeps `tabindex="0"`, all others `-1`, and `aria-selected` plus `aria-controls` stay in sync.
- Quick filters are one keystroke away: `/` and `Cmd/Ctrl+K` (suppressed while typing in a form field) switch to the Network Overview tab and focus and select the agent directory filter; `Escape` clears the active filter query and re-renders the full directory.
- Every form control has a programmatic `<label>`; asynchronous results (settlements, simulations, passport issuance) are announced through `aria-live` regions instead of `window.alert()`; interactive controls maintain visible focus rings and 4.5:1 text contrast.

**Deterministic 5-State Feedback**: every operational surface renders the same five states, so skeleton geometry never shifts the layout (`CLS = 0`).

| State | Rendering Contract |
| :--- | :--- |
| `Empty` | Neutral empty-state panel with a single next-action hint; no stale rows retained |
| `Loading` | Fixed-geometry skeleton rows/boxes pre-sized to the eventual payload (`CLS = 0`), plus `aria-busy` on the container |
| `Nominal` | Populated data tables and telemetry with monospace, tabular-aligned identifiers and USD figures |
| `Degraded` | `DETERMINISTIC_MULTI_FACTOR` jury fallback callout when the DGX Spark LLM Audit Jury is unreachable, with the arbitration engine surfaced as `data-engine` |
| `Error` | Inline status/error banner carrying the API error code and message, focusable and announced to assistive technology |

---

## 5. HTTP/2 & Connect-Style RPC API Reference

| Method & Path | Description |
| :--- | :--- |
| `GET /{$}` | Serves the embedded 5-tab ACN RaaS & Threat Simulator Web Console shell (`internal/api/ui/index.html`) as `text/html; charset=utf-8` with `X-Content-Type-Options: nosniff` |
| `GET /ui/{asset...}` | Serves the remaining modular console assets (`tokens.css`, `components.css`, `views.js`, `app.js`) with extension-derived MIME types and `Cache-Control: public, max-age=300`; unknown or traversal-style names return `404` |
| `GET /healthz` | Liveness probe (`200 OK`) |
| `GET /ready` | Readiness probe reporting active Merkle root and registered agent count (`200 OK`) |
| `GET /api/v1/overview` | Network telemetry, Merkle root, active agents, escrow TVL, and slash statistics |
| `GET /api/v1/agents` | Lists all Sovereign Root Agents (Tiers 0–3), Merchant Vendors, and Rotating Sub-Agents |
| `POST /api/v1/agents` | Registers a Root DID (`register_root`), spawns an unlinkable Sub-Agent (`spawn_sub`), or deposits bond (`deposit_bond`) |
| `POST /api/v1/relay/book` | Executes SIP-style Agent-to-Agent handshake (`INITIATED -> REPUTATION_VERIFIED -> ESCROW_LOCKED -> CONFIRMED`) |
| `POST /api/v1/oracle/challenge` | Issues a 60-second replay-protected cryptographic challenge nonce |
| `POST /api/v1/oracle/settle` | Executes Double-Blind settlement (`STATUS_SUCCESS`, `STATUS_SLASHED`, or `STATUS_DISPUTED` with AI Jury arbitration) |
| `POST /api/v1/raas/passport` | Issues an enclave-signed selective-disclosure `TrustPassport` anchored to the active `MerkleRoot` |
| `POST /api/v1/raas/verify` | Verifies a `TrustPassport` cryptographic signature and validity window |
| `GET /api/v1/ledger/merkle` | Inspects the SHA-256 Merkle root, interaction batch log, and `InclusionProof` (`?index=0`) |
| `GET /api/v1/governance/proposals` | Lists active constitutional proposals and current vote weights |
| `POST /api/v1/governance/vote` | Casts a Reputation-Weighted vote ($V_i = \ln(1+S_i) \times R_i$) or validator veto |
| `POST /api/v1/simulate/{scenario}` | Runs deterministic threat simulations (`sybil-swarm`, `whale-attack`, `collusive-vendor`) |

---

## 6. Local Verification & Build Instructions

```bash
# 1. Verify formatting, module integrity, static analysis, and race-enabled unit tests
test -z "$(gofmt -l .)"
go mod verify
go vet ./...
go test -v -race -cover ./...

# 2. Validate Kubernetes Kustomize manifests
kubectl kustomize deploy/base

# 3. Compile the static, CGO-free production binary (M18 gate)
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o dist/acn-node ./cmd/acn-node

# 4. Run the ACN Control-Plane Node locally on port 8000
go run ./cmd/acn-node

# 5. Smoke-test the embedded console and its modular assets
curl -fsS http://127.0.0.1:8000/ | head -n 20
curl -fsSI http://127.0.0.1:8000/ui/tokens.css
curl -fsSI http://127.0.0.1:8000/ui/components.css
curl -fsSI http://127.0.0.1:8000/ui/views.js
curl -fsSI http://127.0.0.1:8000/ui/app.js
```
