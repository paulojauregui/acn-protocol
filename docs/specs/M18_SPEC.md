# Milestone M18 Specification: Documentation, Static Binary Build & Kustomize Verification (`v2-ux`)

## 1. Objective
Update `ROADMAP.md` and `README.md` to document Iteration 2 (`v2-ux`) milestones `M13`–`M18` (modular `embed.FS` UI architecture, `docs/UX_SPEC.md`, `docs/DESIGN_SYSTEM.md`, WCAG 2.2 AA keyboard navigation, and 5-state determinism), and verify static binary compilation (`CGO_ENABLED=0 go build ./cmd/acn-node`), full race-detector test suite (`go test -v -race -cover ./...`), and Kustomize manifest generation (`kubectl kustomize deploy/base`).

## 2. Target Files
1. `ROADMAP.md`
2. `README.md`

## 3. Detailed Contract
- In `ROADMAP.md`:
  - Add Iteration 2 (`v2-ux`) Milestone Matrix (`M13` through `M18`) documenting deliverables and verification gates.
- In `README.md`:
  - Document the modular `internal/api/ui/` architecture (`index.html`, `tokens.css`, `components.css`, `views.js`, `app.js` served via `//go:embed ui/*` at `GET /{$}` and `GET /ui/{asset...}`), keyboard navigation shortcuts (`ArrowLeft`/`ArrowRight`/`Home`/`End`, `/` and `Cmd/Ctrl+K` quick filter), and link `docs/UX_SPEC.md` and `docs/DESIGN_SYSTEM.md`.
- Zero emojis anywhere in `ROADMAP.md` or `README.md`.

## 4. Verification Command
```bash
export PATH="/home/tron/.local/go/bin:$PATH" && test -z "$(gofmt -l .)" && go mod verify && go vet ./... && go test -v -race -cover ./... && CGO_ENABLED=0 go build -o /tmp/acn-node ./cmd/acn-node && rm -f /tmp/acn-node && kubectl kustomize deploy/base >/dev/null
```
