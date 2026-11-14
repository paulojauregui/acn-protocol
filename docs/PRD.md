# Product Requirements Document (PRD): Agentic Communication Network (ACN) Protocol

**Document Version:** 1.0.0  
**Status:** Approved  
**Owner:** Principal Product Manager  
**Repository:** `github.com/paulojauregui/acn-protocol`  

---

## 1. Executive Summary & Problem Statement

As autonomous AI agents transition from passive text generation to executing real-world transactions—booking Waymo rides, scheduling UCSF specialist appointments, reserving Michelin-starred dining tables, and negotiating B2B supply contracts—physical and service-sector vendors face an existential threat: **Zero-Cost Agentic Denial-of-Inventory**.

Today, a malicious actor or misconfigured agent swarm can spawn 10,000 ephemeral agents at near-zero marginal cost and reserve every scarce appointment slot in a city. Conversely, human users deploying personal sovereign agents face privacy erosion if every vendor interaction links back to a static global identifier, and face unfair slashing if a negligent or collusive merchant falsely marks an arrived user as a "No-Show."

The **Agentic Communication Network (ACN)** solves this trilemma (**Sybil Resistance**, **Sovereign Privacy**, and **Double-Blind Physical Accountability**) via a cryptographic control plane, a Weight-of-Stake and Time-Weighted Velocity Reputation Engine, a Blind Escrow Vault backed by Stripe ACS Proof-of-Capital, and a Double-Blind Accountability Oracle.

---

## 2. Target Personas & Use Cases

| Persona | Primary Objective | Key Threat / Pain Point | ACN Protocol Solution |
| :--- | :--- | :--- | :--- |
| **Sovereign Personal Agent** | Book rides, medical screenings, and dining slots autonomously without leaking cross-vendor behavioral graphs. | Tracking across vendors; false "No-Show" accusations by faulty vendor systems. | Rotating ephemeral `SubAgent` DIDs (`did:acn:sub:<id>`), Selective-Disclosure `TrustPassport`, and 60s Double-Blind Check-In. |
| **High-Value Service Vendor** (`Waymo`, `UCSF`, `Michelin Dining`) | Accept autonomous agent bookings with guaranteed fulfillment or immediate financial compensation. | Sybil inventory exhaustion, Whale flash-attacks, and uncompensated no-shows. | SIP-style Pre-Answer Gatekeeper (`MinRequiredScore`, `RequiredTrustTier`) + automatic `70%` vendor payout on verified `NO_SHOW`. |
| **Validator & Audit Jury Node** | Maintain ledger integrity and arbitrate conflicting presence claims. | Ambiguous dispute evidence or offline AI arbitration nodes. | Deterministic multi-factor consensus (`StripePOSReceipt` + `LocalBeaconVerified`) with `< 2.0s` DGX Spark LLM jury integration and instant deterministic fallback. |

---

## 3. Core Functional Requirements

### 3.1 Sovereign Identity, DIDs & 4-Tier Trust Ladder (`M02`)
1. **Root Sovereign Identity**: Every participant registers a `RootAgent` identified by `did:acn:<hex_pubkey_fingerprint>` backed by an `Ed25519` keypair, optional Proof-of-Personhood (`PoPVerified`), and TEE hardware enclave attestation (`TEEAttested`).
2. **Unlinkable Rotating Sub-Agents**: A `RootAgent` spawns single-use `SubAgent` addresses (`did:acn:sub:<id>`) cryptographically bound to the `RootDID` inside the registry. Vendors interact only with the `SubAgent` DID, preventing cross-vendor correlation, while all escrow locks, slashes, and reputation updates roll up atomically to the `RootAgent`.
3. **4-Tier Trust Ladder**:
   - **Tier 0 (Ephemeral)**: Max booking exposure `<= $5.00` (unbonded new agents).
   - **Tier 1 (Economic)**: Max booking exposure `<= $150.00` (requires `>= $50.00` Good-Behavior Bond OR `>= 100` settled Tier-0 transactions).
   - **Tier 2 (Reputational)**: Max booking exposure `<= $2,000.00` (requires `>= $500.00` Stripe Proof-of-Spend across `>= 20` settled transactions + active bond).
   - **Tier 3 (Sovereign)**: Unrestricted exposure (requires `PoPVerified == true`, `Tier 2` prerequisites, and composite trust score `>= 4.50`).

### 3.2 Sybil-Resistant & Whale-Resistant Reputation Engine (`M03`)
1. **4-Component Composite Score (`0.00` to `5.00`)**:
   - **Fulfillment Rate** (Weight `0.50`)
   - **Cancellation Lead-Time** (Weight `0.20`, normalized across a 24-hour notice window)
   - **Peer Review Rating** (Weight `0.20`)
   - **Stake Seniority Age** (Weight `0.10`, logarithmic/bounded maturity over 180 days)
2. **Weight-of-Stake ($\omega$) Endorsement**:
   $$T_{\text{new}} = \text{clamp}\left(T_{\text{old}} + \frac{\sum (R_i \times S_i \times V_i)}{N \times \text{ScaleFactor}}, 0.0, 5.0\right)$$
   Ensures zero-stake Sybil bots ($S_i = 0$) or low-reputation colluders ($R_i \approx 0$) contribute zero endorsement lift.
3. **Time-Weighted Velocity ($\nu$) Whale Defense**:
   $$\text{VelocityCapacity} = (\text{StakeUSD} \times \text{AccountAgeDays}) + (\text{SettledTxCount} \times \text{QualityWeight})$$
   Prevents a freshly created account with `$1,000,000` in capital (`AccountAgeDays` $\approx 0$) from booking hundreds of concurrent slots before building longitudinal settlement history.
4. **Right to be Forgotten & Probation**:
   - Historical slash penalties decay exponentially over time ($e^{-\lambda \Delta t}$).
   - Any agent whose composite score drops below `2.50` enters `PROBATIONARY` status.

### 3.3 Stripe ACS Proof-of-Capital & Blind Escrow Vault (`M04`)
1. **Good-Behavior Bonds & Per-Appointment Locks**: Supports depositing Good-Behavior Bonds, recording verified Stripe ACS Proof-of-Spend, and locking per-appointment commitment bonds (`LockBond`, `ReleaseBond`, `SlashBond`).
2. **Fee & Slashing Economics**:
   - **Successful Settlement (`STATUS_SUCCESS`)**: Deducts a `0.5%` Network Integrity Fee and releases `99.5%` of the locked commitment bond back to the user (or applies toward settlement).
   - **Verified No-Show (`STATUS_SLASHED`)**: Slashes `100%` of the locked commitment bond (`70%` compensated directly to the aggrieved Vendor, `30%` retained by the ACN Protocol Treasury as an Arbitration Fee). If the locked bond is insufficient, any deficit is deducted from the Root Agent's Good-Behavior Bond.

### 3.4 Double-Blind Accountability Oracle & 60s Nonce Handshake (`M05`)
1. **Replay-Protected 60s Challenge Nonce**: Issues cryptographic nonces (`IssueChallengeNonce`) with a strict 60-second TTL and an in-memory replay cache.
2. **Double-Blind Mutual Attestation (`SettleAppointment`)**:
   - Both User and Vendor attest presence (`UserPresent && VendorPresent`) -> `STATUS_SUCCESS` (releases bond, increments reputation, awards Vendor Honest-Reporter micro-reward).
   - Confirmed user absence (`!UserPresent && !VendorPresent`, or timeout without user proof) -> `STATUS_SLASHED`.
   - Conflicting claims (`UserPresent != VendorPresent`) -> `STATUS_DISPUTED` (routes to Audit Jury; if multi-factor evidence proves user presence, protects user and slashes the fabricating Vendor's Merchant Reputation).

### 3.5 Merkle Ledger Batcher & Privacy-Preserving RaaS Passports (`M06`)
1. **Deterministic SHA-256 Merkle Tree**: Batches signed settlement records into a binary Merkle tree with O(log N) inclusion proofs (`ComputeMerkleRoot`, `GenerateInclusionProof`, `VerifyInclusionProof`).
2. **Selective-Disclosure Trust Passports**: Issues signed cryptographic credentials proving boolean predicates (`MinFulfillmentRate >= 0.95`, `MinSettledBookings >= N`, `MinTrustTier >= T`) anchored to the active `MerkleRoot` and TEE enclave signature without exposing historical counterparties.

### 3.6 Governance Constitution & DGX Spark AI Audit Jury (`M07`)
1. **Reputation-Weighted Voting**: $V_i = \ln(1 + S_i) \times R_i$, balancing capital stake with proven behavioral reputation, guarded by Validator Node veto checks.
2. **Hybrid Deterministic + AI Audit Jury**: Evaluates `STATUS_DISPUTED` settlements via an OpenAI-compatible HTTP client targeting `SPARK_LLM_BASE_URL` (`http://happy.tail749110.ts.net:8000/v1/chat/completions`, model `nvidia/Qwen3.8-Flash-Next-FP4-Local`) under a `< 2.0s` timeout with instant deterministic multi-factor fallback.

### 3.7 High-Concurrency Agent Relay & Threat Simulator (`M08`–`M11`)
1. **SIP-Style Pre-Answer Router**: Enforces `INITIATED -> REPUTATION_VERIFIED -> ESCROW_LOCKED -> CONFIRMED | REJECTED_LOW_TRUST`.
2. **Adversarial Threat Model Simulator**: Deterministic execution of *1,000-Bot Sybil Swarm*, *\$1M Whale Attack*, and *Collusive Vendor Audit*.
3. **Embedded 5-Tab RaaS & Threat Console**: Served at `GET /` via Go `//go:embed` alongside REST/Connect-style JSON endpoints on port `8000`.

---

## 4. Non-Functional & Operational Invariants

- **Runtime**: Go `1.27.4`, `CGO_ENABLED=0`, zero external C dependencies.
- **Test Latency**: Full race-enabled test suite (`go test -v -race -cover ./...`) executes in `< 2.0s` with zero external network calls.
- **Container Security**: Multi-stage Docker image running as `USER 10001:10001` on port `8000` with `/healthz` and `/ready` probes.
