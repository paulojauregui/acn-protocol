# Milestone M14 Specification: Go `embed.FS` Multi-File Static Asset Serving (`v2-ux`)

## 1. Objective
Upgrade `internal/api/server.go` and `internal/api/handlers.go` from single-file `//go:embed ui/index.html` to a multi-file embedded filesystem (`//go:embed ui/*` via `embed.FS`) that serves:
1. `GET /{$}` -> `ui/index.html` with `Content-Type: text/html; charset=utf-8` and `X-Content-Type-Options: nosniff`.
2. `GET /ui/{asset...}` -> static assets from `ui/` (`tokens.css`, `components.css`, `app.js`, `views.js`, etc.) via `io/fs` (`fs.Sub`) with deterministic MIME content types (`text/css; charset=utf-8` for `.css`, `application/javascript; charset=utf-8` for `.js`, `text/html; charset=utf-8` for `.html`), `Cache-Control: public, max-age=300`, and `X-Content-Type-Options: nosniff`. Returns `404 Not Found` for nonexistent assets or path traversal attempts.

## 2. Target Files
1. `internal/api/server.go`
2. `internal/api/handlers.go`
3. `internal/api/ui_assets_test.go`

## 3. Detailed Contract
- In `internal/api/server.go`:
  - Replace `_ "embed"` with `"embed"`.
  - Declare `//go:embed ui/*` on `var uiFS embed.FS`.
  - In `registerRoutes()`, register:
    - `s.Mux.HandleFunc("GET /{$}", s.handleRoot)`
    - `s.Mux.HandleFunc("GET /ui/{asset...}", s.handleUIAsset)`
- In `internal/api/handlers.go`:
  - Update `handleRoot(w http.ResponseWriter, r *http.Request)` to read `ui/index.html` from `uiFS` (`uiFS.ReadFile("ui/index.html")`), set `Content-Type: text/html; charset=utf-8` and `X-Content-Type-Options: nosniff`, and write `200 OK`.
  - Add `handleUIAsset(w http.ResponseWriter, r *http.Request)` with full DbC doc comments (`Pre-conditions:` and `Post-conditions:`):
    - Extracts `asset := r.PathValue("asset")`. Rejects empty paths, paths containing `".."`, or leading/trailing slashes with `404 Not Found`.
    - Reads `"ui/" + asset` from `uiFS.ReadFile("ui/" + asset)`; if not found, returns `404 Not Found`.
    - Sets `X-Content-Type-Options: nosniff` and `Cache-Control: public, max-age=300`.
    - Sets `Content-Type`:
      - `.css` -> `text/css; charset=utf-8`
      - `.js` -> `application/javascript; charset=utf-8`
      - `.html` -> `text/html; charset=utf-8`
    - Writes `200 OK` and the asset bytes.
- In `internal/api/ui_assets_test.go`:
  - Add HTTP handler tests verifying `GET /ui/tokens.css` and `GET /ui/components.css` return `200 OK`, `Content-Type: text/css; charset=utf-8`, `X-Content-Type-Options: nosniff`, and `Cache-Control: public, max-age=300`, and `GET /ui/nonexistent.css` returns `404 Not Found`.

## 4. Verification Command
```bash
export PATH="/home/tron/.local/go/bin:$PATH" && test -z "$(gofmt -l .)" && go vet ./... && go test -v -race ./internal/api/...
```
