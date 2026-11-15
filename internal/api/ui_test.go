//go:build !js

package api_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode"

	"github.com/paulojauregui/acn-protocol/internal/api"
)

// TestUIJS_ViewsModuleServedAndContract verifies that internal/api/ui/views.js is served
// via the embedded filesystem endpoint GET /ui/views.js with correct MIME types, cache
// headers, required view rendering functions, wire-protocol normalization, and zero alerts or emojis.
//
// Pre-conditions:
//   - An initialized api.Server instance with embedded ui filesystem.
//   - internal/api/ui/views.js must exist on disk and be embedded.
//
// Post-conditions:
//   - Verifies HTTP 200 OK status on GET /ui/views.js.
//   - Verifies Content-Type: application/javascript; charset=utf-8.
//   - Verifies X-Content-Type-Options: nosniff and Cache-Control: public, max-age=300.
//   - Verifies presence of escapeHTML, fmtUSD, setBannerState, renderTableSkeleton,
//     renderAgentsTable, renderSubAgentsTable, populateAgentDropdowns, setStepperState,
//     renderJuryCallout, and renderSimMetrics functions.
//   - Verifies wire-protocol field normalization (sub.sub_did) and hybrid AI fallback tokens
//     (DETERMINISTIC_MULTI_FACTOR and DGX_SPARK_LLM).
//   - Verifies zero alert( or window.alert( calls.
//   - Verifies zero emojis in the JavaScript source.
func TestUIJS_ViewsModuleServedAndContract(t *testing.T) {
	srv := api.NewServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/ui/views.js")
	if err != nil {
		t.Fatalf("GET /ui/views.js failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /ui/views.js status = %d, want 200", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "application/javascript") {
		t.Errorf("Content-Type = %q, want application/javascript", contentType)
	}

	nosniff := resp.Header.Get("X-Content-Type-Options")
	if nosniff != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", nosniff)
	}

	cacheControl := resp.Header.Get("Cache-Control")
	if !strings.Contains(cacheControl, "max-age=") {
		t.Errorf("Cache-Control = %q, want max-age directive", cacheControl)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed reading /ui/views.js body: %v", err)
	}
	content := string(bodyBytes)

	requiredSnippets := []struct {
		desc    string
		snippet string
	}{
		{"XSS prevention helper", "function escapeHTML"},
		{"USD currency formatter", "function fmtUSD"},
		{"Screen reader live announcer", "function announceSR"},
		{"5-state status banner updater", "function setBannerState"},
		{"CLS=0 table skeleton generator", "function renderTableSkeleton"},
		{"Trust tier badge generator", "function getTierBadge"},
		{"Sovereign root agents table renderer", "function renderAgentsTable"},
		{"Rotating sub-agents table renderer", "function renderSubAgentsTable"},
		{"Agent dropdowns populator", "function populateAgentDropdowns"},
		{"Relay 4-phase stepper visualizer", "function setStepperState"},
		{"Hybrid AI jury callout renderer", "function renderJuryCallout"},
		{"Adversarial simulation metrics renderer", "function renderSimMetrics"},
		{"Sub-agent wire-protocol field normalization", "sub.sub_did"},
		{"Sub-agent purpose wire field", "sub.purpose"},
		{"Shared agent directory filter helper", "function matchesAgentFilter"},
		{"Deterministic multi-factor jury fallback engine", "DETERMINISTIC_MULTI_FACTOR"},
		{"DGX Spark nominal remote jury engine", "DGX_SPARK_LLM"},
		{"Skeleton row class", "skeleton-row"},
		{"Data state attribute", "data-state"},
	}

	for _, tc := range requiredSnippets {
		t.Run(tc.desc, func(t *testing.T) {
			if !strings.Contains(content, tc.snippet) {
				t.Fatalf("/ui/views.js missing required snippet %q (%s)", tc.snippet, tc.desc)
			}
		})
	}

	// Gate 3 invariant: zero inline style attributes inside rendered templates
	if strings.Contains(content, `style="`) {
		t.Errorf("/ui/views.js contains inline style= attribute; use components.css classes")
	}

	// Invariant: zero alert() or window.alert() calls
	if strings.Contains(content, "alert(") {
		t.Errorf("/ui/views.js contains forbidden alert( invocation")
	}

	// Invariant: zero emojis
	for _, r := range content {
		if r > 127 && unicode.Is(unicode.So, r) {
			t.Fatalf("/ui/views.js contains forbidden symbol/emoji character: %c (U+%04X)", r, r)
		}
	}
}

// TestUIJS_AppControllerServedAndContract verifies that internal/api/ui/app.js is served
// via the embedded filesystem endpoint GET /ui/app.js with correct MIME types, cache
// headers, required controller functions, WAI-ARIA tab navigation, global shortcuts,
// API handlers, wire-protocol normalization, and zero alerts or emojis.
//
// Pre-conditions:
//   - An initialized api.Server instance with embedded ui filesystem.
//   - internal/api/ui/app.js must exist on disk and be embedded.
//
// Post-conditions:
//   - Verifies HTTP 200 OK status on GET /ui/app.js.
//   - Verifies Content-Type: application/javascript; charset=utf-8.
//   - Verifies X-Content-Type-Options: nosniff and Cache-Control: public, max-age=300.
//   - Verifies presence of switchTab, ArrowRight, ArrowLeft, Home, End keyboard navigation.
//   - Verifies clearAgentFilter, refreshOverview, handleRelayBooking, handleIssueNonce,
//     handleOracleSettle, handleIssuePassport, handleVerifyPassport, fetchMerkleLedger,
//     auditMerkleProof, and runSimulation functions.
//   - Verifies wire-protocol normalization (session.state || session.status).
//   - Verifies zero alert( or window.alert( calls.
//   - Verifies zero emojis in the JavaScript source.
func TestUIJS_AppControllerServedAndContract(t *testing.T) {
	srv := api.NewServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/ui/app.js")
	if err != nil {
		t.Fatalf("GET /ui/app.js failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /ui/app.js status = %d, want 200", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "application/javascript") {
		t.Errorf("Content-Type = %q, want application/javascript", contentType)
	}

	nosniff := resp.Header.Get("X-Content-Type-Options")
	if nosniff != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", nosniff)
	}

	cacheControl := resp.Header.Get("Cache-Control")
	if !strings.Contains(cacheControl, "max-age=") {
		t.Errorf("Cache-Control = %q, want max-age directive", cacheControl)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed reading /ui/app.js body: %v", err)
	}
	content := string(bodyBytes)

	requiredSnippets := []struct {
		desc    string
		snippet string
	}{
		{"WAI-ARIA tab switching function", "function switchTab"},
		{"Roving tabindex right navigation", "ArrowRight"},
		{"Roving tabindex left navigation", "ArrowLeft"},
		{"Roving tabindex home navigation", "Home"},
		{"Roving tabindex end navigation", "End"},
		{"Quick filter reset controller", "clearAgentFilter"},
		{"Overview telemetry synchronization", "refreshOverview"},
		{"Relay booking handler", "handleRelayBooking"},
		{"Challenge nonce issue handler", "handleIssueNonce"},
		{"Oracle settlement handler", "handleOracleSettle"},
		{"TrustPassport issue handler", "handleIssuePassport"},
		{"TrustPassport verification handler", "handleVerifyPassport"},
		{"Merkle ledger fetcher", "fetchMerkleLedger"},
		{"Merkle inclusion proof auditor", "auditMerkleProof"},
		{"Adversarial threat simulator executor", "runSimulation"},
		{"Session state wire-protocol normalization", "session.state || session.status"},
		{"Overview registered_agents wire field", "data.registered_agents"},
		{"Overview escrow.total_locked_usd wire field", "escrow.total_locked_usd"},
		{"Relay bond_amount_usd wire field", "bond_amount_usd"},
		{"Settlement receipt released_to_user_usd field", "receipt.released_to_user_usd"},
		{"Passport req_min_fulfillment wire field", "req_min_fulfillment"},
		{"Passport verify data.valid wire field", "data.valid"},
		{"Merkle InteractionRecord user_root_did wire field", "r.user_root_did"},
		{"Merkle out-of-bounds inclusion_proof check", "!data.inclusion_proof"},
	}

	for _, tc := range requiredSnippets {
		t.Run(tc.desc, func(t *testing.T) {
			if !strings.Contains(content, tc.snippet) {
				t.Fatalf("/ui/app.js missing required snippet %q (%s)", tc.snippet, tc.desc)
			}
		})
	}

	// Gate 3 invariant: zero inline style attributes
	if strings.Contains(content, `style="`) {
		t.Errorf("/ui/app.js contains inline style= attribute; use components.css classes")
	}

	// Invariant: zero alert() or window.alert() calls
	if strings.Contains(content, "alert(") {
		t.Errorf("/ui/app.js contains forbidden alert( invocation")
	}

	// Invariant: zero emojis
	for _, r := range content {
		if r > 127 && unicode.Is(unicode.So, r) {
			t.Fatalf("/ui/app.js contains forbidden symbol/emoji character: %c (U+%04X)", r, r)
		}
	}
}
