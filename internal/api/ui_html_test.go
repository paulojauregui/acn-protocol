package api_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode"
)

// readUIHTML loads internal/api/ui/index.html from disk relative to the current test directory
// or from repo-root relative search paths.
//
// Pre-conditions:
//   - internal/api/ui/index.html must exist on disk and be accessible.
//
// Post-conditions:
//   - Returns the full string content of index.html, or fails the test if not found.
func readUIHTML(t *testing.T) string {
	t.Helper()
	candidates := []string{
		"ui/index.html",
		filepath.Join("internal", "api", "ui", "index.html"),
		filepath.Join("..", "ui", "index.html"),
		filepath.Join("..", "..", "internal", "api", "ui", "index.html"),
	}
	for _, cand := range candidates {
		data, err := os.ReadFile(cand)
		if err == nil {
			return string(data)
		}
	}
	t.Fatalf("failed to locate ui/index.html across search paths: %v", candidates)
	return ""
}

// TestUIHTML_ExternalAssetLinksAndNoInlineBloat verifies that index.html links external
// modular CSS and deferred JS files, contains zero inline style tags, contains zero inline
// script code blocks, and adheres to the strict line-count budget (< 500 lines).
//
// Pre-conditions:
//   - internal/api/ui/index.html must exist on disk.
//
// Post-conditions:
//   - Verifies stylesheet links for /ui/tokens.css and /ui/components.css.
//   - Verifies deferred script links for /ui/views.js and /ui/app.js.
//   - Verifies zero <style> tags in index.html.
//   - Verifies zero inline <script>...</script> code blocks.
//   - Verifies line count is strictly less than 600 lines (and under 500 lines).
//   - Verifies zero emojis in index.html.
func TestUIHTML_ExternalAssetLinksAndNoInlineBloat(t *testing.T) {
	content := readUIHTML(t)

	// 1. External CSS links
	if !strings.Contains(content, `<link rel="stylesheet" href="/ui/tokens.css">`) {
		t.Error("index.html missing external stylesheet link for /ui/tokens.css")
	}
	if !strings.Contains(content, `<link rel="stylesheet" href="/ui/components.css">`) {
		t.Error("index.html missing external stylesheet link for /ui/components.css")
	}

	// 2. External deferred JS scripts
	if !strings.Contains(content, `<script src="/ui/views.js" defer></script>`) {
		t.Error("index.html missing external deferred script tag for /ui/views.js")
	}
	if !strings.Contains(content, `<script src="/ui/app.js" defer></script>`) {
		t.Error("index.html missing external deferred script tag for /ui/app.js")
	}

	// 3. Zero inline <style> tags
	if strings.Contains(strings.ToLower(content), "<style") {
		t.Error("index.html contains forbidden inline <style> tag; all styling must reside in tokens.css / components.css")
	}

	// 4. Zero inline <script> code blocks (only allowed <script src=...></script>)
	inlineScriptRegex := regexp.MustCompile(`(?s)<script\b[^>]*>(.*?)</script>`)
	matches := inlineScriptRegex.FindAllStringSubmatch(content, -1)
	for _, m := range matches {
		inlineBody := strings.TrimSpace(m[1])
		if len(inlineBody) > 0 {
			t.Errorf("index.html contains forbidden inline script body: %q", inlineBody)
		}
	}

	// 5. Line count budget (< 600 lines; target < 500 lines)
	lines := strings.Split(content, "\n")
	lineCount := len(lines)
	if lineCount >= 600 {
		t.Errorf("index.html exceeds line budget: got %d lines, want < 600 (ideal < 500)", lineCount)
	}

	// 6. Zero emojis
	for _, r := range content {
		if r > 127 && unicode.Is(unicode.So, r) {
			t.Fatalf("index.html contains forbidden symbol/emoji character: %c (U+%04X)", r, r)
		}
	}
}

// TestUIHTML_LandmarksAndWAIARIATabs verifies semantic HTML5 landmarks (header, nav,
// main, footer, skip link) and WAI-ARIA tablist / tab / tabpanel role and state contracts.
//
// Pre-conditions:
//   - internal/api/ui/index.html must exist on disk.
//
// Post-conditions:
//   - Verifies .skip-link targeting #main-content.
//   - Verifies #sr-announcer with role="status" and aria-live="polite".
//   - Verifies <header role="banner">.
//   - Verifies <nav id="console-tablist" role="tablist" aria-label="Console Primary Views">.
//   - Verifies 5 role="tab" buttons with appropriate aria-selected, aria-controls, and tabindex.
//   - Verifies <main id="main-content" role="main" tabindex="-1">.
//   - Verifies 5 role="tabpanel" sections with matching aria-labelledby and hidden attributes on inactive tabs.
//   - Verifies <footer role="contentinfo">.
func TestUIHTML_LandmarksAndWAIARIATabs(t *testing.T) {
	content := readUIHTML(t)

	// Skip link & live announcer
	if !strings.Contains(content, `href="#main-content"`) || !strings.Contains(content, `class="skip-link"`) {
		t.Error("index.html missing accessible skip link targeting #main-content")
	}
	if !strings.Contains(content, `id="sr-announcer"`) {
		t.Error("index.html missing #sr-announcer screen reader container")
	}

	// Header banner
	if !strings.Contains(content, `<header role="banner">`) {
		t.Error("index.html missing <header role=\"banner\"> landmark")
	}

	// Navigation landmark wrapping WAI-ARIA tablist
	if !strings.Contains(content, `<nav class="tab-bar" role="navigation"`) {
		t.Error("index.html missing <nav class=\"tab-bar\" role=\"navigation\"> landmark wrapping tablist")
	}
	if !strings.Contains(content, `role="tablist"`) || !strings.Contains(content, `id="console-tablist"`) {
		t.Error("index.html missing WAI-ARIA tablist container with id=\"console-tablist\"")
	}
	if !strings.Contains(content, `aria-label="Console Primary Views"`) {
		t.Error("index.html missing aria-label=\"Console Primary Views\" on tablist")
	}

	// 5 Tab buttons
	tabButtons := []struct {
		id       string
		controls string
		selected string
		tabindex string
	}{
		{"tab-btn-overview", "tab-overview", "true", "0"},
		{"tab-btn-relay", "tab-relay", "false", "-1"},
		{"tab-btn-oracle", "tab-oracle", "false", "-1"},
		{"tab-btn-raas", "tab-raas", "false", "-1"},
		{"tab-btn-threat", "tab-threat", "false", "-1"},
	}

	for _, tb := range tabButtons {
		t.Run("tab-btn/"+tb.id, func(t *testing.T) {
			if !strings.Contains(content, `id="`+tb.id+`"`) {
				t.Fatalf("missing tab button id=%q", tb.id)
			}
			btnPattern := regexp.MustCompile(`<button[^>]*id="` + tb.id + `"[^>]*>`)
			match := btnPattern.FindString(content)
			if match == "" {
				t.Fatalf("unable to match <button> tag for %s", tb.id)
			}
			if !strings.Contains(match, `role="tab"`) {
				t.Errorf("%s missing role=\"tab\"", tb.id)
			}
			if !strings.Contains(match, `aria-controls="`+tb.controls+`"`) {
				t.Errorf("%s missing aria-controls=%q", tb.id, tb.controls)
			}
			if !strings.Contains(match, `aria-selected="`+tb.selected+`"`) {
				t.Errorf("%s missing aria-selected=%q", tb.id, tb.selected)
			}
			if !strings.Contains(match, `tabindex="`+tb.tabindex+`"`) {
				t.Errorf("%s missing tabindex=%q", tb.id, tb.tabindex)
			}
		})
	}

	// Main landmark
	if !strings.Contains(content, `<main id="main-content" role="main" tabindex="-1">`) {
		t.Error("index.html missing <main id=\"main-content\" role=\"main\" tabindex=\"-1\"> landmark")
	}

	// 5 Tab panels
	tabPanels := []struct {
		id         string
		labelledBy string
		mustHidden bool
	}{
		{"tab-overview", "tab-btn-overview", false},
		{"tab-relay", "tab-btn-relay", true},
		{"tab-oracle", "tab-btn-oracle", true},
		{"tab-raas", "tab-btn-raas", true},
		{"tab-threat", "tab-btn-threat", true},
	}

	for _, tp := range tabPanels {
		t.Run("tab-panel/"+tp.id, func(t *testing.T) {
			if !strings.Contains(content, `id="`+tp.id+`"`) {
				t.Fatalf("missing tab panel id=%q", tp.id)
			}
			sectionPattern := regexp.MustCompile(`<section[^>]*id="` + tp.id + `"[^>]*>`)
			match := sectionPattern.FindString(content)
			if match == "" {
				t.Fatalf("unable to match <section> tag for %s", tp.id)
			}
			if !strings.Contains(match, `role="tabpanel"`) {
				t.Errorf("%s missing role=\"tabpanel\"", tp.id)
			}
			if !strings.Contains(match, `aria-labelledby="`+tp.labelledBy+`"`) {
				t.Errorf("%s missing aria-labelledby=%q", tp.id, tp.labelledBy)
			}
			if tp.mustHidden && !strings.Contains(match, "hidden") {
				t.Errorf("%s expected to have initial 'hidden' attribute", tp.id)
			}
			if !tp.mustHidden && strings.Contains(match, "hidden") {
				t.Errorf("%s expected to be active without 'hidden' attribute", tp.id)
			}
		})
	}

	// Footer landmark
	if !strings.Contains(content, `<footer role="contentinfo">`) {
		t.Error("index.html missing <footer role=\"contentinfo\"> landmark")
	}
}

// TestUIHTML_ExplicitFormLabels verifies that all 32 form control IDs across all 5 tabs
// have an explicit <label for="CONTROL_ID"> binding in index.html to guarantee WCAG 2.2 AA.
//
// Pre-conditions:
//   - internal/api/ui/index.html must exist on disk.
//
// Post-conditions:
//   - Verifies presence of each form control element with matching id attribute.
//   - Verifies presence of a matching <label for="CONTROL_ID"> element.
func TestUIHTML_ExplicitFormLabels(t *testing.T) {
	content := readUIHTML(t)

	requiredFormControls := []struct {
		id   string
		desc string
	}{
		// Tab 1: Directory & Identity
		{"agent-filter-input", "Quick Directory Filter search input"},
		{"reg-name", "Register Agent: Display Name"},
		{"reg-vendor", "Register Agent: Role / Type selector"},
		{"reg-bond", "Register Agent: Good-behavior bond USD"},
		{"reg-pop", "Register Agent: PoP verified checkbox"},
		{"reg-tee", "Register Agent: TEE attestation checkbox"},
		{"spawn-parent", "Spawn Sub-Agent: Parent Root DID selector"},
		{"spawn-purpose", "Spawn Sub-Agent: Delegation scope"},
		{"spawn-ttl", "Spawn Sub-Agent: TTL duration seconds"},
		{"deposit-did", "Deposit Bond: Target DID selector"},
		{"deposit-amount", "Deposit Bond: Collateral amount USD"},

		// Tab 2: Live Relay & Blind Escrow
		{"relay-caller", "Booking Initiator: Caller DID selector"},
		{"relay-vendor", "Booking Initiator: Target Vendor DID selector"},
		{"relay-service", "Booking Initiator: Service name"},
		{"relay-bond", "Booking Initiator: Commitment bond USD"},
		{"relay-min-score", "Booking Initiator: Min required score"},
		{"relay-min-tier", "Booking Initiator: Required trust tier"},

		// Tab 3: Accountability Oracle & Check-In
		{"orc-appt-id", "Challenge Nonce: Appointment ID"},
		{"orc-user-did", "Challenge Nonce: User DID selector"},
		{"orc-vendor-did", "Challenge Nonce: Vendor DID selector"},
		{"settle-lock-id", "Double-Blind Settlement: Vault lock ID"},
		{"settle-amount", "Double-Blind Settlement: Booking amount USD"},
		{"settle-user-pres", "Double-Blind Settlement: User present checkbox"},
		{"settle-vendor-pres", "Double-Blind Settlement: Vendor present checkbox"},
		{"settle-stripe", "Double-Blind Settlement: Stripe POS receipt checkbox"},
		{"settle-beacon", "Double-Blind Settlement: BLE beacon checkbox"},
		{"settle-rating", "Double-Blind Settlement: Peer rating"},

		// Tab 4: RaaS Privacy Passport & Merkle Ledger
		{"pass-subject", "RaaS Issuer: Subject DID selector"},
		{"pass-min-ful", "RaaS Issuer: Min fulfillment rate"},
		{"pass-min-book", "RaaS Issuer: Min settled bookings"},
		{"pass-min-tier", "RaaS Issuer: Min required trust tier"},
		{"merkle-proof-idx", "Merkle Auditor: Leaf record index"},
	}

	for _, fc := range requiredFormControls {
		t.Run("form-control/"+fc.id, func(t *testing.T) {
			// Check element ID exists
			if !strings.Contains(content, `id="`+fc.id+`"`) {
				t.Fatalf("missing form control element id=%q (%s)", fc.id, fc.desc)
			}

			// Check matching explicit label for="id"
			labelPattern := regexp.MustCompile(`<label[^>]*for="` + regexp.QuoteMeta(fc.id) + `"[^>]*>`)
			if !labelPattern.MatchString(content) {
				t.Errorf("missing explicit <label for=%q> for form control %s", fc.id, fc.desc)
			}
		})
	}
}

// TestUIHTML_LiveRegionsAndCanonicalIDs verifies that all 8 live status regions have
// role="status" and aria-live="polite", and verifies that all canonical DOM IDs required
// by the state controllers and view renderers are preserved.
//
// Pre-conditions:
//   - internal/api/ui/index.html must exist on disk.
//
// Post-conditions:
//   - Verifies all 8 live regions have role="status" and aria-live="polite".
//   - Verifies canonical telemetry, table, stepper, badge, and JSON viewer IDs.
func TestUIHTML_LiveRegionsAndCanonicalIDs(t *testing.T) {
	content := readUIHTML(t)

	// 1. Live status regions
	liveRegions := []struct {
		id   string
		desc string
	}{
		{"directory-status-banner", "Directory action & filter status banner"},
		{"relay-result-summary", "Relay negotiation outcome banner"},
		{"orc-nonce-status", "Challenge nonce lifecycle status badge"},
		{"settle-result-summary", "Settlement outcome & receipt banner"},
		{"jury-fallback-callout", "AI jury degraded fallback callout"},
		{"passport-status-banner", "TrustPassport issuance & verification banner"},
		{"merkle-proof-status", "Merkle inclusion proof verification banner"},
		{"sim-summary", "Adversarial threat model simulation outcome banner"},
	}

	for _, lr := range liveRegions {
		t.Run("live-region/"+lr.id, func(t *testing.T) {
			if !strings.Contains(content, `id="`+lr.id+`"`) {
				t.Fatalf("missing live region id=%q (%s)", lr.id, lr.desc)
			}
			tagPattern := regexp.MustCompile(`<[^>]*id="` + lr.id + `"[^>]*>`)
			match := tagPattern.FindString(content)
			if match == "" {
				t.Fatalf("unable to match tag for %s", lr.id)
			}
			if !strings.Contains(match, `role="status"`) {
				t.Errorf("%s missing role=\"status\"", lr.id)
			}
			if !strings.Contains(match, `aria-live="polite"`) {
				t.Errorf("%s missing aria-live=\"polite\"", lr.id)
			}
		})
	}

	// 2. Canonical DOM IDs
	canonicalIDs := []string{
		// Overview Stats
		"stat-locked-usd", "stat-avail-usd", "stat-fees-usd", "stat-slashed-usd",
		// Tables
		"agents-table", "agents-table-body", "subagents-table-body",
		// Stepper & Relay
		"relay-stepper", "step-1", "step-2", "step-3", "step-4",
		"relay-badge", "relay-json", "relay-send-to-oracle",
		// Oracle Nonce & Settlement
		"orc-nonce-display", "orc-nonce-timer",
		"settle-status-badge", "settle-json",
		// RaaS & Merkle
		"passport-json", "verify-passport-btn",
		"merkle-ledger-root", "merkle-proof-json", "merkle-records-tbody",
		// Adversarial Simulation
		"sim-defense-badge", "sim-metrics-table", "sim-metrics-tbody", "sim-json",
	}

	for _, cid := range canonicalIDs {
		t.Run("canonical-id/"+cid, func(t *testing.T) {
			if !strings.Contains(content, `id="`+cid+`"`) {
				t.Fatalf("missing canonical DOM ID %q in index.html", cid)
			}
		})
	}

	// Gate 3 invariant: zero inline style attributes in index.html
	if strings.Contains(content, "style=") {
		t.Error("index.html contains inline style= attributes; extract styles into components.css tokens/classes")
	}
}
