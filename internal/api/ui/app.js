/**
 * ACN Protocol Console Application Controller (v2-ux)
 * Aesthetic: Sovereign Cryptographic Trust-and-Escrow Mission Control
 * Invariants: Complete WAI-ARIA tab navigation, global keyboard shortcuts,
 *             deterministic 5-state feedback, zero blocking dialogs, zero emojis.
 */

// 1. Application State Store
let cachedRootAgents = [];
let cachedSubAgents = [];
let activeFilterQuery = '';
let activeNonceInterval = null;
let activeNonceValue = '';
let lastConfirmedRelaySession = null;
let lastIssuedPassport = null;
let currentMerkleRoot = '';

// Expose state globally for external inspectability and test helpers
window.acnState = {
  get cachedRootAgents() { return cachedRootAgents; },
  get cachedSubAgents() { return cachedSubAgents; },
  get activeFilterQuery() { return activeFilterQuery; },
  get activeNonceValue() { return activeNonceValue; },
  get lastConfirmedRelaySession() { return lastConfirmedRelaySession; },
  get lastIssuedPassport() { return lastIssuedPassport; },
  get currentMerkleRoot() { return currentMerkleRoot; }
};
window.lastConfirmedRelaySession = null;
window.activeNonceValue = '';

// 2. WAI-ARIA Tab Switching & Roving Tabindex
function switchTab(tabId, btn, focusBtn) {
  const tablist = document.getElementById('console-tablist');
  const tabButtons = tablist ? Array.from(tablist.querySelectorAll('[role="tab"]')) : [];
  const tabPanels = Array.from(document.querySelectorAll('.tab-content[role="tabpanel"]'));

  let activeBtn = btn;
  if (!activeBtn) {
    activeBtn = tabButtons.find(b => b.getAttribute('aria-controls') === tabId) ||
                document.getElementById('tab-btn-' + tabId.replace('tab-', ''));
  }

  // Update tab buttons
  tabButtons.forEach(b => {
    const isSelected = (b === activeBtn);
    b.setAttribute('aria-selected', isSelected ? 'true' : 'false');
    b.setAttribute('tabindex', isSelected ? '0' : '-1');
    if (isSelected) {
      b.classList.add('active');
    } else {
      b.classList.remove('active');
    }
  });

  // Update tab panels
  tabPanels.forEach(p => {
    if (p.id === tabId) {
      p.hidden = false;
      p.removeAttribute('hidden');
      p.classList.add('active');
    } else {
      p.hidden = true;
      p.setAttribute('hidden', '');
      p.classList.remove('active');
    }
  });

  if (focusBtn && activeBtn) {
    activeBtn.focus();
  }

  const tabTitle = activeBtn ? activeBtn.textContent.trim() : tabId;
  announceSR(`Switched to tab: ${tabTitle}`);
}

// 3. Tablist Keyboard Navigation (ArrowRight, ArrowLeft, Home, End)
function initTablistKeyboardNav() {
  const tablist = document.getElementById('console-tablist');
  if (!tablist) return;

  tablist.addEventListener('keydown', (e) => {
    const tabButtons = Array.from(tablist.querySelectorAll('[role="tab"]'));
    const currentIndex = tabButtons.indexOf(document.activeElement);
    if (currentIndex === -1) return;

    let targetIndex = -1;
    if (e.key === 'ArrowRight') {
      e.preventDefault();
      targetIndex = (currentIndex + 1) % tabButtons.length;
    } else if (e.key === 'ArrowLeft') {
      e.preventDefault();
      targetIndex = (currentIndex - 1 + tabButtons.length) % tabButtons.length;
    } else if (e.key === 'Home') {
      e.preventDefault();
      targetIndex = 0;
    } else if (e.key === 'End') {
      e.preventDefault();
      targetIndex = tabButtons.length - 1;
    }

    if (targetIndex !== -1) {
      const nextBtn = tabButtons[targetIndex];
      const targetPanelId = nextBtn.getAttribute('aria-controls');
      switchTab(targetPanelId, nextBtn, true);
    }
  });
}

// 4. Global Keyboard Shortcuts (/ for filter, Cmd/Ctrl+K, Escape to clear)
function initGlobalShortcuts() {
  document.addEventListener('keydown', (e) => {
    const activeEl = document.activeElement;
    const isFormInput = activeEl && (
      ['INPUT', 'SELECT', 'TEXTAREA'].includes(activeEl.tagName) ||
      activeEl.isContentEditable
    );

    // '/' shortcut when not typing in an input
    const isSlashShortcut = (e.key === '/' && !isFormInput);
    // Cmd+K / Ctrl+K shortcut
    const isCmdKShortcut = ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k');

    if (isSlashShortcut || isCmdKShortcut) {
      e.preventDefault();
      const overviewBtn = document.getElementById('tab-btn-overview');
      switchTab('tab-overview', overviewBtn, false);
      const filterInput = document.getElementById('agent-filter-input');
      if (filterInput) {
        filterInput.focus();
        filterInput.select();
      }
      return;
    }

    // Escape shortcut
    if (e.key === 'Escape') {
      if ((activeEl && activeEl.id === 'agent-filter-input') || activeFilterQuery) {
        e.preventDefault();
        clearAgentFilter();
      }
    }
  });
}

// 5. Quick Directory Filter Controller
function filterAgents(query) {
  activeFilterQuery = String(query || '').trim();
  renderAgentsTable(cachedRootAgents, activeFilterQuery);

  const total = cachedRootAgents.length;
  if (!activeFilterQuery) {
    setBannerState('directory-status-banner', 'nominal', `Directory ready. Showing all ${total} registered agents.`, false);
  } else {
    const q = activeFilterQuery.toLowerCase();
    const matches = cachedRootAgents.filter(item => {
      const a = item.agent || item;
      const name = (a.display_name || '').toLowerCase();
      const did = (a.root_did || '').toLowerCase();
      const tier = `tier ${a.trust_tier || 0}`.toLowerCase();
      const role = a.is_vendor ? 'vendor' : 'user';
      return name.includes(q) || did.includes(q) || tier.includes(q) || role.includes(q);
    }).length;
    setBannerState('directory-status-banner', 'nominal', `Filter active: ${matches} of ${total} agents match "${activeFilterQuery}".`, false);
  }
}

function clearAgentFilter() {
  const input = document.getElementById('agent-filter-input');
  if (input) {
    input.value = '';
    input.blur();
  }
  activeFilterQuery = '';
  renderAgentsTable(cachedRootAgents, '');
  setBannerState('directory-status-banner', 'nominal', `Filter cleared. Showing all ${cachedRootAgents.length} registered agents.`, false);
}

// 6. API Client: Tab 1 Network Overview & Directory
async function refreshOverview() {
  const pillStatus = document.getElementById('pill-status');
  if (pillStatus) {
    pillStatus.textContent = 'SYNCING...';
    pillStatus.className = 'pill-val blue';
  }

  try {
    const res = await fetch('/api/v1/overview');
    if (!res.ok) {
      throw new Error(`HTTP ${res.status}: ${res.statusText}`);
    }
    const data = await res.json();

    currentMerkleRoot = data.merkle_root || '';
    const rootShort = currentMerkleRoot ? currentMerkleRoot.substring(0, 10) + '...' : 'EMPTY';

    const elMerklePill = document.getElementById('pill-merkle-root');
    if (elMerklePill) elMerklePill.textContent = rootShort;

    const elRegPill = document.getElementById('pill-registered-agents');
    if (elRegPill) elRegPill.textContent = String(data.registered_root_agents || 0);

    const elSubPill = document.getElementById('pill-sub-agents');
    if (elSubPill) elSubPill.textContent = String(data.active_sub_agents || 0);

    const elLockPill = document.getElementById('pill-escrow-locked');
    if (elLockPill) elLockPill.textContent = fmtUSD(data.total_locked_escrow_usd);

    const elFeesPill = document.getElementById('pill-treasury-fees');
    if (elFeesPill) elFeesPill.textContent = fmtUSD(data.total_treasury_fees_usd);

    const elStatLocked = document.getElementById('stat-locked-usd');
    if (elStatLocked) elStatLocked.textContent = fmtUSD(data.total_locked_escrow_usd);

    const elStatAvail = document.getElementById('stat-avail-usd');
    if (elStatAvail) elStatAvail.textContent = fmtUSD(data.total_available_collateral_usd);

    const elStatFees = document.getElementById('stat-fees-usd');
    if (elStatFees) elStatFees.textContent = fmtUSD(data.total_treasury_fees_usd);

    const elStatSlashed = document.getElementById('stat-slashed-usd');
    if (elStatSlashed) elStatSlashed.textContent = fmtUSD(data.total_slashed_collateral_usd);

    const elLedgerRoot = document.getElementById('merkle-ledger-root');
    if (elLedgerRoot) elLedgerRoot.textContent = currentMerkleRoot || 'EMPTY';

    if (pillStatus) {
      pillStatus.textContent = 'ONLINE';
      pillStatus.className = 'pill-val green';
      pillStatus.setAttribute('data-state', 'nominal');
    }

    await Promise.all([fetchAgents(), fetchMerkleLedger()]);
  } catch (err) {
    if (pillStatus) {
      pillStatus.textContent = 'DEGRADED';
      pillStatus.className = 'pill-val amber';
      pillStatus.setAttribute('data-state', 'degraded');
    }
    setBannerState('directory-status-banner', 'error', `Overview telemetry synchronization failed: ${err.message}`, true);
  }
}

async function fetchAgents() {
  renderTableSkeleton('agents-table-body', 5, 4);
  renderTableSkeleton('subagents-table-body', 4, 2);

  try {
    const res = await fetch('/api/v1/agents');
    if (!res.ok) {
      throw new Error(`HTTP ${res.status}: ${res.statusText}`);
    }
    const data = await res.json();

    cachedRootAgents = data.root_agents || [];
    cachedSubAgents = data.sub_agents || [];

    renderAgentsTable(cachedRootAgents, activeFilterQuery);
    renderSubAgentsTable(cachedSubAgents);
    populateAgentDropdowns(cachedRootAgents, cachedSubAgents);
  } catch (err) {
    setBannerState('directory-status-banner', 'error', `Failed to load agent directory: ${err.message}`, true);
  }
}

function selectAgentForDeposit(did) {
  const depositSelect = document.getElementById('deposit-did');
  if (depositSelect) {
    depositSelect.value = did;
  }
  const amtInput = document.getElementById('deposit-amount');
  if (amtInput) {
    amtInput.focus();
    amtInput.select();
  }
  announceSR(`Selected agent ${did} for collateral deposit.`);
}

async function handleRegisterAgent(e) {
  if (e && e.preventDefault) e.preventDefault();
  setBannerState('directory-status-banner', 'loading', 'Registering Sovereign Root Agent...', false);

  const payload = {
    action: 'register_root',
    display_name: (document.getElementById('reg-name')?.value || '').trim(),
    is_vendor: document.getElementById('reg-vendor')?.value === 'true',
    deposit_bond_usd: parseFloat(document.getElementById('reg-bond')?.value) || 0,
    pop_verified: Boolean(document.getElementById('reg-pop')?.checked),
    tee_attested: Boolean(document.getElementById('reg-tee')?.checked)
  };

  try {
    const res = await fetch('/api/v1/agents', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    });
    const data = await res.json();
    if (!res.ok) {
      throw new Error(data.error || `HTTP ${res.status}`);
    }

    const regNameInput = document.getElementById('reg-name');
    if (regNameInput) regNameInput.value = '';

    setBannerState('directory-status-banner', 'nominal', `Successfully registered agent "${data.agent?.display_name || payload.display_name}".`, false);
    await refreshOverview();
  } catch (err) {
    setBannerState('directory-status-banner', 'error', `Agent registration failed: ${err.message}`, true);
  }
}

async function handleSpawnSubAgent(e) {
  if (e && e.preventDefault) e.preventDefault();
  setBannerState('directory-status-banner', 'loading', 'Spawning unlinkable rotating sub-agent...', false);

  const payload = {
    action: 'spawn_sub',
    root_did: document.getElementById('spawn-parent')?.value || '',
    purpose: (document.getElementById('spawn-purpose')?.value || '').trim(),
    ttl_seconds: parseInt(document.getElementById('spawn-ttl')?.value, 10) || 3600
  };

  try {
    const res = await fetch('/api/v1/agents', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    });
    const data = await res.json();
    if (!res.ok) {
      throw new Error(data.error || `HTTP ${res.status}`);
    }

    const subDID = data.sub_agent?.sub_did || data.sub_agent?.sub_agent_did || 'new sub-agent';
    setBannerState('directory-status-banner', 'nominal', `Successfully delegated sub-agent DID: ${subDID}`, false);
    await refreshOverview();
  } catch (err) {
    setBannerState('directory-status-banner', 'error', `Sub-agent delegation failed: ${err.message}`, true);
  }
}

async function handleDepositBond(e) {
  if (e && e.preventDefault) e.preventDefault();
  setBannerState('directory-status-banner', 'loading', 'Depositing collateral bond to vault...', false);

  const payload = {
    action: 'deposit_bond',
    root_did: document.getElementById('deposit-did')?.value || '',
    amount_usd: parseFloat(document.getElementById('deposit-amount')?.value) || 0
  };

  try {
    const res = await fetch('/api/v1/agents', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    });
    const data = await res.json();
    if (!res.ok) {
      throw new Error(data.error || `HTTP ${res.status}`);
    }

    setBannerState('directory-status-banner', 'nominal', `Successfully deposited ${fmtUSD(payload.amount_usd)} collateral.`, false);
    await refreshOverview();
  } catch (err) {
    setBannerState('directory-status-banner', 'error', `Collateral deposit failed: ${err.message}`, true);
  }
}

// 7. API Client: Tab 2 Live Agent Relay & Blind Escrow Negotiator
async function handleRelayBooking(e) {
  if (e && e.preventDefault) e.preventDefault();
  setStepperState('INITIATED');
  setBannerState('relay-result-summary', 'loading', 'Initiating SIP pre-answer booking negotiation...', false);

  const badge = document.getElementById('relay-badge');
  if (badge) {
    badge.className = 'badge badge-t1';
    badge.textContent = 'NEGOTIATING...';
    badge.setAttribute('data-state', 'loading');
  }

  const payload = {
    caller_did: document.getElementById('relay-caller')?.value || '',
    vendor_did: document.getElementById('relay-vendor')?.value || '',
    service_name: (document.getElementById('relay-service')?.value || '').trim(),
    bond_amount_usd: parseFloat(document.getElementById('relay-bond')?.value) || 0,
    min_required_score: parseFloat(document.getElementById('relay-min-score')?.value) || 0,
    required_trust_tier: parseInt(document.getElementById('relay-min-tier')?.value, 10) || 0
  };

  const jsonViewer = document.getElementById('relay-json');
  const sendBtn = document.getElementById('relay-send-to-oracle');

  try {
    const res = await fetch('/api/v1/relay/book', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    });
    const session = await res.json();

    if (jsonViewer) {
      jsonViewer.textContent = JSON.stringify(session, null, 2);
    }

    // Authoritative wire normalization: session.state || session.status
    const sessionState = session.state || session.status || '';

    if (!res.ok) {
      setStepperState('FAILED');
      if (badge) {
        badge.className = 'badge badge-danger';
        badge.textContent = 'ERROR';
        badge.setAttribute('data-state', 'error');
      }
      setBannerState('relay-result-summary', 'error', `Booking negotiation failed: ${session.error || res.statusText}`, true);
      if (sendBtn) sendBtn.style.display = 'none';
      return;
    }

    if (sessionState === 'CONFIRMED') {
      setStepperState('CONFIRMED');
      if (badge) {
        badge.className = 'badge badge-success';
        badge.textContent = 'CONFIRMED';
        badge.setAttribute('data-state', 'nominal');
      }
      lastConfirmedRelaySession = session;
      window.lastConfirmedRelaySession = session;
      setBannerState('relay-result-summary', 'nominal', `Booking confirmed. Lock ID: ${session.lock_id}. Sealed escrow: ${fmtUSD(session.locked_bond_usd || payload.bond_amount_usd)}. Fee: ${fmtUSD(session.protocol_fee_usd)}.`, false);
      if (sendBtn) sendBtn.style.display = 'inline-block';
    } else {
      setStepperState('REJECTED_LOW_TRUST');
      if (badge) {
        badge.className = 'badge badge-danger';
        badge.textContent = sessionState || 'REJECTED_LOW_TRUST';
        badge.setAttribute('data-state', 'degraded');
      }
      setBannerState('relay-result-summary', 'degraded', `Booking handshake rejected: ${session.rejection_reason || 'Trust gate threshold not satisfied.'}`, false);
      if (sendBtn) sendBtn.style.display = 'none';
    }

    await refreshOverview();
  } catch (err) {
    setStepperState('FAILED');
    if (badge) {
      badge.className = 'badge badge-danger';
      badge.textContent = 'TRANSPORT ERROR';
      badge.setAttribute('data-state', 'error');
    }
    setBannerState('relay-result-summary', 'error', `Booking transport error: ${err.message}`, true);
    if (sendBtn) sendBtn.style.display = 'none';
  }
}

// Aliases matching HTML inline onsubmit handlers
function handleBookRelay(e) {
  return handleRelayBooking(e);
}

function prefillOracleFromRelay() {
  const s = lastConfirmedRelaySession || window.lastConfirmedRelaySession;
  if (!s) return;

  const lockInput = document.getElementById('settle-lock-id');
  if (lockInput) lockInput.value = s.lock_id || '';

  const amtInput = document.getElementById('settle-amount');
  if (amtInput && s.locked_bond_usd) amtInput.value = s.locked_bond_usd;

  const userSelect = document.getElementById('orc-user-did');
  if (userSelect && s.caller_did) userSelect.value = s.caller_did;

  const vendorSelect = document.getElementById('orc-vendor-did');
  if (vendorSelect && s.vendor_did) vendorSelect.value = s.vendor_did;

  const oracleTabBtn = document.getElementById('tab-btn-oracle');
  switchTab('tab-oracle', oracleTabBtn, true);
  announceSR(`Transferred lock ID ${s.lock_id} to Accountability Oracle.`);
}

function transferRelayToOracle() {
  prefillOracleFromRelay();
}

// 8. API Client: Tab 3 Accountability Oracle & Check-In
async function handleIssueNonce(e) {
  if (e && e.preventDefault) e.preventDefault();

  const payload = {
    appointment_id: (document.getElementById('orc-appt-id')?.value || '').trim(),
    user_did: document.getElementById('orc-user-did')?.value || '',
    vendor_did: document.getElementById('orc-vendor-did')?.value || ''
  };

  const nonceDisplay = document.getElementById('orc-nonce-display');
  const statusEl = document.getElementById('orc-nonce-status');

  try {
    const res = await fetch('/api/v1/oracle/challenge', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    });
    const data = await res.json();
    if (!res.ok) {
      throw new Error(data.error || `HTTP ${res.status}`);
    }

    activeNonceValue = data.nonce || '';
    window.activeNonceValue = activeNonceValue;

    if (nonceDisplay) nonceDisplay.textContent = activeNonceValue;
    startNonceTimer(60);
    announceSR(`Issued 60-second cryptographic challenge nonce: ${activeNonceValue}`);
  } catch (err) {
    if (statusEl) {
      statusEl.className = 'badge badge-danger';
      statusEl.textContent = 'ERROR';
      statusEl.setAttribute('data-state', 'error');
    }
    setBannerState('settle-result-summary', 'error', `Failed to issue challenge nonce: ${err.message}`, true);
  }
}

function startNonceTimer(seconds) {
  if (activeNonceInterval) {
    clearInterval(activeNonceInterval);
    activeNonceInterval = null;
  }

  let rem = Number(seconds) || 60;
  const timerEl = document.getElementById('orc-nonce-timer');
  const statusEl = document.getElementById('orc-nonce-status');

  if (statusEl) {
    statusEl.className = 'badge badge-success';
    statusEl.textContent = 'ACTIVE';
    statusEl.setAttribute('data-state', 'nominal');
  }
  if (timerEl) timerEl.textContent = `${rem}s`;

  activeNonceInterval = setInterval(() => {
    rem--;
    if (rem <= 0) {
      clearInterval(activeNonceInterval);
      activeNonceInterval = null;
      if (timerEl) timerEl.textContent = 'EXPIRED';
      if (statusEl) {
        statusEl.className = 'badge badge-danger';
        statusEl.textContent = 'EXPIRED';
        statusEl.setAttribute('data-state', 'error');
      }
      announceSR('Cryptographic challenge nonce expired.');
    } else {
      if (timerEl) timerEl.textContent = `${rem}s`;
    }
  }, 1000);
}

function setSettlementScenario(type) {
  const userPres = document.getElementById('settle-user-pres');
  const vendorPres = document.getElementById('settle-vendor-pres');
  const stripePos = document.getElementById('settle-stripe');
  const beacon = document.getElementById('settle-beacon');
  const rating = document.getElementById('settle-rating');

  if (type === 'success') {
    if (userPres) userPres.checked = true;
    if (vendorPres) vendorPres.checked = true;
    if (stripePos) stripePos.checked = true;
    if (beacon) beacon.checked = true;
    if (rating) rating.value = '5.0';
    announceSR('Loaded scenario preset: Mutual Arrival (Success).');
  } else if (type === 'noshow' || type === 'slash') {
    if (userPres) userPres.checked = false;
    if (vendorPres) vendorPres.checked = true;
    if (stripePos) stripePos.checked = false;
    if (beacon) beacon.checked = false;
    if (rating) rating.value = '1.0';
    announceSR('Loaded scenario preset: User No-Show (Slash Collateral).');
  } else if (type === 'dispute') {
    if (userPres) userPres.checked = true;
    if (vendorPres) vendorPres.checked = false;
    if (stripePos) stripePos.checked = true;
    if (beacon) beacon.checked = true;
    if (rating) rating.value = '5.0';
    announceSR('Loaded scenario preset: Vendor Dispute (AI Jury Arbitration).');
  }
}

function presetSettlement(type) {
  setSettlementScenario(type);
}

async function handleOracleSettle(e) {
  if (e && e.preventDefault) e.preventDefault();
  setBannerState('settle-result-summary', 'loading', 'Adjudicating settlement against protocol oracle...', false);

  const badge = document.getElementById('settle-status-badge');
  if (badge) {
    badge.className = 'badge badge-t1';
    badge.textContent = 'ADJUDICATING...';
    badge.setAttribute('data-state', 'loading');
  }

  const nonceVal = activeNonceValue || (document.getElementById('orc-nonce-display')?.textContent || '').trim();

  const payload = {
    appointment_id: (document.getElementById('orc-appt-id')?.value || '').trim(),
    lock_id: (document.getElementById('settle-lock-id')?.value || '').trim(),
    nonce: nonceVal,
    user_did: document.getElementById('orc-user-did')?.value || '',
    vendor_did: document.getElementById('orc-vendor-did')?.value || '',
    user_present: Boolean(document.getElementById('settle-user-pres')?.checked),
    vendor_present: Boolean(document.getElementById('settle-vendor-pres')?.checked),
    stripe_pos_receipt: Boolean(document.getElementById('settle-stripe')?.checked),
    local_beacon_verified: Boolean(document.getElementById('settle-beacon')?.checked),
    peer_rating: parseFloat(document.getElementById('settle-rating')?.value) || 5.0,
    amount_usd: parseFloat(document.getElementById('settle-amount')?.value) || 25.0
  };

  const jsonViewer = document.getElementById('settle-json');

  try {
    const res = await fetch('/api/v1/oracle/settle', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    });
    const data = await res.json();

    if (jsonViewer) {
      jsonViewer.textContent = JSON.stringify(data, null, 2);
    }

    if (!res.ok) {
      if (badge) {
        badge.className = 'badge badge-danger';
        badge.textContent = 'SETTLEMENT FAILED';
        badge.setAttribute('data-state', 'error');
      }
      setBannerState('settle-result-summary', 'error', `Settlement execution failed: ${data.error || res.statusText}`, true);
      renderJuryCallout(null);
      return;
    }

    const outcome = data.outcome || {};
    renderJuryCallout(data.verdict);

    if (outcome.status === 'STATUS_SUCCESS') {
      if (badge) {
        badge.className = 'badge badge-success';
        badge.textContent = 'STATUS_SUCCESS (MUTUAL ARRIVAL)';
        badge.setAttribute('data-state', 'nominal');
      }
      setBannerState('settle-result-summary', 'nominal', `Mutual arrival confirmed. Released: ${fmtUSD(outcome.released_to_vendor_usd)}. Refunded: ${fmtUSD(outcome.refunded_to_user_usd)}. Merkle record appended.`, false);
    } else if (outcome.status === 'STATUS_SLASHED') {
      if (badge) {
        badge.className = 'badge badge-danger';
        badge.textContent = 'STATUS_SLASHED (NO-SHOW)';
        badge.setAttribute('data-state', 'error');
      }
      setBannerState('settle-result-summary', 'error', `User no-show recorded. Slashed: ${fmtUSD(outcome.slashed_collateral_usd)}. Transferred to vendor: ${fmtUSD(outcome.released_to_vendor_usd)}.`, false);
    } else if (outcome.status === 'STATUS_DISPUTED') {
      if (badge) {
        badge.className = 'badge badge-warning';
        const winner = data.verdict ? data.verdict.winner : 'ARBITRATED';
        badge.textContent = `STATUS_DISPUTED (JURY: ${winner})`;
        badge.setAttribute('data-state', 'degraded');
      }
      setBannerState('settle-result-summary', 'degraded', `Dispute resolved via AI Jury. Released to vendor: ${fmtUSD(outcome.released_to_vendor_usd)}. Refunded to user: ${fmtUSD(outcome.refunded_to_user_usd)}.`, false);
    }

    await refreshOverview();
  } catch (err) {
    if (badge) {
      badge.className = 'badge badge-danger';
      badge.textContent = 'TRANSPORT ERROR';
      badge.setAttribute('data-state', 'error');
    }
    setBannerState('settle-result-summary', 'error', `Settlement transport error: ${err.message}`, true);
    renderJuryCallout(null);
  }
}

function handleSettle(e) {
  return handleOracleSettle(e);
}

// 9. API Client: Tab 4 RaaS Privacy Passport & Merkle Ledger
async function handleIssuePassport(e) {
  if (e && e.preventDefault) e.preventDefault();
  setBannerState('passport-status-banner', 'loading', 'Evaluating enclave predicates & issuing TrustPassport...', false);

  const payload = {
    subject_did: document.getElementById('pass-subject')?.value || '',
    min_fulfillment_rate: parseFloat(document.getElementById('pass-min-ful')?.value) || 0.8,
    min_settled_bookings: parseInt(document.getElementById('pass-min-book')?.value, 10) || 2,
    min_trust_tier: parseInt(document.getElementById('pass-min-tier')?.value, 10) || 2
  };

  const jsonViewer = document.getElementById('passport-json');
  const verifyBtn = document.getElementById('verify-passport-btn');

  try {
    const res = await fetch('/api/v1/raas/passport', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    });
    const data = await res.json();

    if (jsonViewer) {
      jsonViewer.textContent = JSON.stringify(data, null, 2);
    }

    if (!res.ok) {
      setBannerState('passport-status-banner', 'error', `Passport issuance rejected: ${data.error || res.statusText}`, true);
      if (verifyBtn) verifyBtn.style.display = 'none';
      return;
    }

    lastIssuedPassport = data.passport || data;
    window.lastIssuedPassport = lastIssuedPassport;

    setBannerState('passport-status-banner', 'nominal', `TrustPassport issued. ID: ${lastIssuedPassport.passport_id}. Enclave ECDSA signature sealed.`, false);
    if (verifyBtn) verifyBtn.style.display = 'inline-block';
  } catch (err) {
    setBannerState('passport-status-banner', 'error', `Passport issuance transport error: ${err.message}`, true);
    if (verifyBtn) verifyBtn.style.display = 'none';
  }
}

async function handleVerifyPassport() {
  const passport = lastIssuedPassport || window.lastIssuedPassport;
  if (!passport) {
    setBannerState('passport-status-banner', 'error', 'No active TrustPassport in session to verify.', true);
    return;
  }

  setBannerState('passport-status-banner', 'loading', 'Verifying enclave ECDSA signature against Merkle ledger root...', false);

  try {
    const res = await fetch('/api/v1/raas/verify', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ passport })
    });
    const data = await res.json();

    if (!res.ok || !data.verified) {
      setBannerState('passport-status-banner', 'error', `Verification failed: ${data.error || 'Cryptographic signature invalid.'}`, true);
      return;
    }

    // Check if passport was anchored against a historical Merkle root vs current head
    const passportRoot = data.passport?.merkle_root || passport.merkle_root || '';
    if (currentMerkleRoot && passportRoot && passportRoot !== currentMerkleRoot) {
      setBannerState('passport-status-banner', 'degraded', `VERIFIED (HISTORICAL SNAPSHOT): Valid enclave ECDSA signature under prior Merkle root (${passportRoot.substring(0, 10)}...).`, false);
    } else {
      setBannerState('passport-status-banner', 'nominal', `VERIFIED: Valid enclave ECDSA attestation under Merkle root ${passportRoot.substring(0, 10)}...`, false);
    }
  } catch (err) {
    setBannerState('passport-status-banner', 'error', `Verification request error: ${err.message}`, true);
  }
}

async function fetchMerkleLedger() {
  try {
    const res = await fetch('/api/v1/ledger/merkle');
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    const data = await res.json();

    currentMerkleRoot = data.merkle_root || '';
    const elRoot = document.getElementById('merkle-ledger-root');
    if (elRoot) elRoot.textContent = currentMerkleRoot || 'EMPTY';

    const tbody = document.getElementById('merkle-records-tbody');
    if (!tbody) return;

    if (!data.records || data.records.length === 0) {
      tbody.innerHTML = '<tr><td colspan="6" style="text-align: center; color: var(--text-dim); padding: var(--space-4);">No Merkle interaction records logged.</td></tr>';
      return;
    }

    tbody.innerHTML = data.records.map((r, idx) => {
      let statusBadge = `<span class="badge badge-t1">${escapeHTML(r.status || '')}</span>`;
      if ((r.status || '').includes('SUCCESS')) statusBadge = `<span class="badge badge-success">${escapeHTML(r.status)}</span>`;
      if ((r.status || '').includes('SLASHED')) statusBadge = `<span class="badge badge-danger">${escapeHTML(r.status)}</span>`;
      if ((r.status || '').includes('DISPUTED')) statusBadge = `<span class="badge badge-warning">${escapeHTML(r.status)}</span>`;

      return `
        <tr>
          <td class="tabular-nums">${idx}</td>
          <td><span class="code-span tabular-nums">${escapeHTML(r.record_id || 'rec-' + idx)}</span></td>
          <td>${statusBadge}</td>
          <td><span class="code-span tabular-nums">${escapeHTML(r.caller_did ? r.caller_did.substring(0, 16) + '...' : '-')}</span></td>
          <td class="tabular-nums" style="font-weight: 600;">${fmtUSD(r.amount_usd)}</td>
          <td class="tabular-nums" style="font-size: 11px; color: var(--text-dim);">${r.timestamp ? new Date(r.timestamp).toLocaleTimeString() : 'N/A'}</td>
        </tr>`;
    }).join('');
  } catch (err) {
    console.error('Failed to fetch Merkle ledger:', err);
  }
}

async function auditMerkleProof(e) {
  if (e && e.preventDefault) e.preventDefault();
  const idx = document.getElementById('merkle-proof-idx')?.value || '0';
  setBannerState('merkle-proof-status', 'loading', `Auditing SHA-256 inclusion proof for leaf index ${idx}...`, false);

  const jsonViewer = document.getElementById('merkle-proof-json');

  try {
    const res = await fetch(`/api/v1/ledger/merkle?index=${encodeURIComponent(idx)}`);
    const data = await res.json();

    if (jsonViewer) {
      jsonViewer.textContent = JSON.stringify(data.inclusion_proof || data, null, 2);
    }

    if (!res.ok) {
      setBannerState('merkle-proof-status', 'error', `Audit failed: ${data.error || res.statusText}`, true);
      return;
    }

    setBannerState('merkle-proof-status', 'nominal', `Cryptographic proof verified. Leaf index ${idx} anchored to root ${data.merkle_root?.substring(0, 10)}...`, false);
  } catch (err) {
    setBannerState('merkle-proof-status', 'error', `Merkle audit request error: ${err.message}`, true);
  }
}

function handleAuditMerkleProof(e) {
  return auditMerkleProof(e);
}

// 10. API Client: Tab 5 Adversarial Threat Model Lab
async function runSimulation(scenario) {
  const badge = document.getElementById('sim-defense-badge');
  if (badge) {
    badge.className = 'badge badge-t1';
    badge.textContent = 'SIMULATING...';
    badge.setAttribute('data-state', 'loading');
  }

  setBannerState('sim-summary', 'loading', `Executing deterministic adversarial simulation: ${scenario}...`, false);
  renderTableSkeleton('sim-metrics-tbody', 5, 5);

  const jsonViewer = document.getElementById('sim-json');

  try {
    const res = await fetch(`/api/v1/simulate/${encodeURIComponent(scenario)}`, {
      method: 'POST'
    });
    const data = await res.json();

    if (jsonViewer) {
      jsonViewer.textContent = JSON.stringify(data, null, 2);
    }

    if (!res.ok) {
      if (badge) {
        badge.className = 'badge badge-danger';
        badge.textContent = 'SIMULATION ERROR';
        badge.setAttribute('data-state', 'error');
      }
      setBannerState('sim-summary', 'error', `Simulation failed: ${data.error || res.statusText}`, true);
      return;
    }

    const duration = data.duration_ms ? `${data.duration_ms.toFixed(2)} ms` : 'sub-millisecond';
    const succeeded = Boolean(data.defense_succeeded);

    if (badge) {
      badge.className = succeeded ? 'badge badge-success' : 'badge badge-danger';
      badge.textContent = succeeded ? 'DEFENSE SUCCEEDED [PASS]' : 'DEFENSE BREACHED [FAIL]';
      badge.setAttribute('data-state', succeeded ? 'nominal' : 'error');
    }

    const isDegradedJury = (scenario === 'collusive-vendor' && data.metrics?.jury_engine === 'DETERMINISTIC_MULTI_FACTOR');
    const stateStr = isDegradedJury ? 'degraded' : (succeeded ? 'nominal' : 'error');

    setBannerState('sim-summary', stateStr, `Scenario "${data.scenario}" completed in ${duration}. ${data.summary || ''}`, !succeeded);
    renderSimMetrics(scenario, data.metrics || {});
  } catch (err) {
    if (badge) {
      badge.className = 'badge badge-danger';
      badge.textContent = 'TRANSPORT ERROR';
      badge.setAttribute('data-state', 'error');
    }
    setBannerState('sim-summary', 'error', `Simulation transport error: ${err.message}`, true);
  }
}

// 11. Initial Application Bootstrap
document.addEventListener('DOMContentLoaded', () => {
  initTablistKeyboardNav();
  initGlobalShortcuts();
  refreshOverview();
});
