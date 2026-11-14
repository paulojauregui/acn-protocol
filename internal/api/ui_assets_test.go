package api_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode"
)

// readUIAsset loads a UI stylesheet from disk relative to the current test directory
// or from repo-root relative search paths.
//
// Pre-conditions:
//   - relPath must be a non-empty relative path to a stylesheet asset (e.g. "ui/tokens.css").
//
// Post-conditions:
//   - Returns the full string content of the asset file, or fails the test if not found.
func readUIAsset(t *testing.T, relPath string) string {
	t.Helper()
	candidates := []string{
		relPath,
		filepath.Join("ui", filepath.Base(relPath)),
		filepath.Join("internal", "api", relPath),
		filepath.Join("..", relPath),
		filepath.Join("..", "..", "internal", "api", relPath),
	}
	for _, cand := range candidates {
		data, err := os.ReadFile(cand)
		if err == nil {
			return string(data)
		}
	}
	t.Fatalf("failed to locate UI asset %q across candidates: %v", relPath, candidates)
	return ""
}

// TestUIAssets_TokensCSS verifies that the tokens.css stylesheet defines the full
// sovereign cryptographic trust-and-escrow design token hierarchy.
//
// Pre-conditions:
//   - internal/api/ui/tokens.css must exist on disk and be readable.
//
// Post-conditions:
//   - Verifies 4-tier surface elevation custom properties (--bg-canvas, --bg-surface-1, etc.).
//   - Verifies border, focus ring, typography, and contrast tokens (--text-dim: #8b9bb4).
//   - Verifies 4-tier trust badge tokens (--badge-t0-bg through --badge-t3-bg).
//   - Verifies protocol status tokens (--status-degraded-bg, --status-ok-bg, etc.).
//   - Verifies spatial scale, radius, and transition timing tokens.
//   - Verifies zero emojis in the token stylesheet.
func TestUIAssets_TokensCSS(t *testing.T) {
	content := readUIAsset(t, "ui/tokens.css")
	if len(content) == 0 {
		t.Fatal("tokens.css is empty")
	}

	requiredTokens := []struct {
		name     string
		expected string
	}{
		// 4-Tier Surface Elevation
		{"--bg-canvas", "#070b12"},
		{"--bg-surface-1", "#0d131f"},
		{"--bg-surface-2", "#131c2e"},
		{"--bg-surface-3", "#1a263d"},
		{"--bg-input", "#090e17"},
		{"--bg-code", "#05080e"},
		{"--bg-dark", "var(--bg-canvas)"},
		{"--bg-panel", "var(--bg-surface-1)"},
		{"--bg-card", "var(--bg-surface-2)"},

		// Borders & Focus Rings
		{"--border-subtle", "#1b263b"},
		{"--border-default", "#24324d"},
		{"--border-strong", "#33476b"},
		{"--border-focus", "#38bdf8"},
		{"--focus-ring", "0 0 0 2px rgba(56, 189, 248, 0.45)"},
		{"--border-color", "var(--border-default)"},
		{"--border-active", "var(--border-focus)"},

		// Typography & Contrast (WCAG 2.2 AA verified)
		{"--text-main", "#f1f5f9"},
		{"--text-muted", "#94a3b8"},
		{"--text-dim", "#8b9bb4"},
		{"--text-on-accent", "#04101e"},
		{"--font-sans", "-apple-system, BlinkMacSystemFont"},
		{"--font-mono", "ui-monospace, SFMono-Regular"},
		{"--text-2xs", "0.625rem"},
		{"--text-xs", "0.6875rem"},
		{"--text-sm", "0.75rem"},
		{"--text-base", "0.8125rem"},
		{"--text-md", "0.875rem"},
		{"--text-lg", "1.125rem"},
		{"--text-xl", "1.25rem"},

		// Primary Accents
		{"--accent-blue", "#38bdf8"},
		{"--accent-blue-hover", "#7dd3fc"},
		{"--accent-green", "#4ade80"},
		{"--accent-amber", "#fbbf24"},
		{"--accent-red", "#f87171"},
		{"--accent-purple", "#c084fc"},

		// Semantic Trust Tiers
		{"--badge-t0-bg", "#162032"},
		{"--badge-t0-fg", "#cbd5e1"},
		{"--badge-t0-border", "#334155"},
		{"--badge-t1-bg", "#082f49"},
		{"--badge-t1-fg", "#38bdf8"},
		{"--badge-t1-border", "#0369a1"},
		{"--badge-t2-bg", "#2e1065"},
		{"--badge-t2-fg", "#d8b4fe"},
		{"--badge-t2-border", "#6b21a8"},
		{"--badge-t3-bg", "#052e16"},
		{"--badge-t3-fg", "#4ade80"},
		{"--badge-t3-border", "#166534"},
		{"--badge-vendor-bg", "#451a03"},
		{"--badge-vendor-fg", "#fdba74"},
		{"--badge-vendor-border", "#9a3412"},

		// Semantic Protocol Status
		{"--status-ok-bg", "#052e16"},
		{"--status-danger-bg", "#450a0a"},
		{"--status-warn-bg", "#451a03"},
		{"--status-degraded-bg", "#2e1905"},
		{"--status-info-bg", "#082f49"},

		// Spatial Scale & Motion
		{"--space-1", "0.25rem"},
		{"--space-2", "0.5rem"},
		{"--space-3", "0.75rem"},
		{"--space-4", "1rem"},
		{"--space-5", "1.25rem"},
		{"--space-6", "1.5rem"},
		{"--space-8", "2rem"},
		{"--radius-sm", "4px"},
		{"--radius-md", "6px"},
		{"--radius-lg", "8px"},
		{"--ease-out-expo", "cubic-bezier(0.16, 1, 0.3, 1)"},
		{"--duration-fast", "140ms"},
	}

	for _, tc := range requiredTokens {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(content, tc.name) {
				t.Fatalf("tokens.css missing token declaration %q", tc.name)
			}
			if !strings.Contains(content, tc.expected) {
				t.Fatalf("tokens.css missing expected value %q for token %q", tc.expected, tc.name)
			}
		})
	}

	// Verify zero emojis
	for _, r := range content {
		if r > 127 && unicode.Is(unicode.So, r) {
			t.Fatalf("tokens.css contains forbidden symbol/emoji character: %c (U+%04X)", r, r)
		}
	}
}

// TestUIAssets_ComponentsCSS verifies that components.css implements all required
// layout primitives, accessibility utilities, tabular-nums alignment, 5-state banners,
// CLS=0 skeletons, stepper, jury callouts, and responsive breakpoints.
//
// Pre-conditions:
//   - internal/api/ui/components.css must exist on disk and be readable.
//
// Post-conditions:
//   - Verifies accessibility classes (.sr-only, .skip-link, :focus-visible).
//   - Verifies font-variant-numeric: tabular-nums rule and required selectors.
//   - Verifies 5-state status banner selectors (.status-banner[data-state="..."]).
//   - Verifies zero-CLS skeleton selectors (.skeleton-block, .skeleton-row).
//   - Verifies 4-phase stepper and hybrid AI jury callout selectors.
//   - Verifies reduced-motion query and responsive breakpoint queries.
//   - Verifies zero emojis in the component stylesheet.
func TestUIAssets_ComponentsCSS(t *testing.T) {
	content := readUIAsset(t, "ui/components.css")
	if len(content) == 0 {
		t.Fatal("components.css is empty")
	}

	requiredSnippets := []struct {
		desc    string
		snippet string
	}{
		// Accessibility
		{"sr-only utility", ".sr-only"},
		{"skip-link utility", ".skip-link"},
		{"focus-visible hairline halo", ":focus-visible"},

		// Tabular numbers
		{"tabular-nums property", "font-variant-numeric: tabular-nums"},
		{"tabular-nums utility class", ".tabular-nums"},
		{"stat-val numeric alignment", ".stat-val"},
		{"pill-val numeric alignment", ".pill-val"},
		{"code-span numeric alignment", ".code-span"},
		{"badge numeric alignment", ".badge"},
		{"step-circle numeric alignment", ".step-circle"},
		{"table cell numeric alignment", "td.num"},
		{"table header numeric alignment", "th.num"},
		{"oracle nonce timer selector", "#orc-nonce-timer"},

		// Semantic layout
		{"header masthead", "header"},
		{"brand container", ".brand"},
		{"top telemetry pills", ".top-telemetry"},
		{"tab bar navigation", "nav.tab-bar"},
		{"tab button active state", ".tab-btn[aria-selected=\"true\"]"},
		{"main landmark", "main"},
		{"tab content panel", ".tab-content"},
		{"grid-2 layout", ".grid-2"},
		{"grid-3 layout", ".grid-3"},
		{"grid-4 layout", ".grid-4"},
		{"card container", ".card"},
		{"stat box container", ".stat-box"},
		{"footer landmark", "footer"},

		// Badges & Tables
		{"table responsive wrapper", ".table-responsive"},
		{"table element", "table"},
		{"table th header", "th"},
		{"table td cell", "td"},
		{"badge t0 ephemeral", ".badge-t0"},
		{"badge t1 economic", ".badge-t1"},
		{"badge t2 reputational", ".badge-t2"},
		{"badge t3 sovereign", ".badge-t3"},
		{"badge vendor merchant", ".badge-vendor"},
		{"badge success", ".badge-success"},
		{"badge danger", ".badge-danger"},
		{"badge warning", ".badge-warning"},
		{"badge degraded fallback", ".badge-degraded"},

		// Forms & Buttons
		{"filter toolbar", ".filter-toolbar"},
		{"form control input", ".form-control"},
		{"primary button", ".btn"},
		{"secondary button", ".btn-secondary"},
		{"danger button", ".btn-danger"},
		{"json viewer preformatted block", "pre.json-viewer"},

		// 5-State Status Banner
		{"status-banner base", ".status-banner"},
		{"status-banner empty state", `.status-banner[data-state="empty"]`},
		{"status-banner loading state", `.status-banner[data-state="loading"]`},
		{"status-banner nominal state", `.status-banner[data-state="nominal"]`},
		{"status-banner degraded state", `.status-banner[data-state="degraded"]`},
		{"status-banner error state", `.status-banner[data-state="error"]`},

		// Skeletons
		{"skeleton block", ".skeleton-block"},
		{"skeleton row", ".skeleton-row"},
		{"skeleton pulse animation", "@keyframes acn-skeleton-pulse"},

		// Stepper & Jury Callout
		{"stepper container", ".stepper"},
		{"step node", ".step-node"},
		{"step circle", ".step-circle"},
		{"step title", ".step-title"},
		{"jury callout base", ".jury-callout"},
		{"jury callout deterministic fallback engine", `.jury-callout[data-engine="DETERMINISTIC_MULTI_FACTOR"]`},

		// Adversarial Attack Cards
		{"attack card container", ".attack-card"},
		{"attack card header", ".attack-header"},
		{"attack card title", ".attack-title"},
		{"attack card description", ".attack-desc"},
		{"attack card defense badge", ".attack-defense"},

		// Motion & Responsive Queries
		{"reduced-motion preference", "@media (prefers-reduced-motion: reduce)"},
		{"tablet responsive breakpoint", "@media (max-width: 1023px)"},
		{"mobile responsive breakpoint", "@media (max-width: 767px)"},
		{"ultrawide responsive constraint", "@media (min-width: 1600px)"},
	}

	for _, tc := range requiredSnippets {
		t.Run(tc.desc, func(t *testing.T) {
			if !strings.Contains(content, tc.snippet) {
				t.Fatalf("components.css missing required snippet %q (%s)", tc.snippet, tc.desc)
			}
		})
	}

	// Verify zero emojis
	for _, r := range content {
		if r > 127 && unicode.Is(unicode.So, r) {
			t.Fatalf("components.css contains forbidden symbol/emoji character: %c (U+%04X)", r, r)
		}
	}
}

// TestUIAssets_ComponentsNoRawHexColors verifies that components.css strictly consumes
// CSS custom properties from tokens.css and contains zero raw hex color literals
// (e.g. #fff, #123456) in style rules, ensuring design system token compliance.
//
// Pre-conditions:
//   - internal/api/ui/components.css must exist on disk and be readable.
//
// Post-conditions:
//   - Verifies that zero CSS property values or declarations contain hardcoded hex color literals.
func TestUIAssets_ComponentsNoRawHexColors(t *testing.T) {
	content := readUIAsset(t, "ui/components.css")
	if len(content) == 0 {
		t.Fatal("components.css is empty")
	}

	// Strip CSS comments before scanning for raw hex colors
	commentRegex := regexp.MustCompile(`(?s)/\*.*?\*/`)
	cleanCSS := commentRegex.ReplaceAllString(content, "")

	// Split by line to inspect declarations
	lines := strings.Split(cleanCSS, "\n")

	// Match hex color patterns: '#' followed by 3, 4, 6, or 8 hexadecimal digits
	// in property values (after a colon), ensuring ID selectors like #orc-nonce-timer
	// or non-color hashes are not falsely flagged.
	hexColorRegex := regexp.MustCompile(`:\s*[^;]*#[0-9a-fA-F]{3,8}\b`)

	for lineIdx, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "/*") || strings.HasPrefix(trimmed, "*") {
			continue
		}
		if hexColorRegex.MatchString(trimmed) {
			t.Errorf("line %d: detected raw hex color literal in components.css: %q (must use var(--token))",
				lineIdx+1, trimmed)
		}
	}
}
