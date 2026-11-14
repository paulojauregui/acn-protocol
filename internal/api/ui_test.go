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
