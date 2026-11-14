# Milestone M12 Specification: Hardened Container, Kustomize GitOps Manifests, Full QA & Autonomous PR #1

## 1. Objective
Package `acn-protocol` for Kubernetes deployment on `mainframe` (`192.168.13.100`) via a multi-stage `CGO_ENABLED=0` `Dockerfile` (`USER 10001:10001` on port `8000`), Kustomize base manifests (`deploy/base/`), comprehensive `README.md`, full verification suite, and Pull Request #1 on `paulojauregui/acn-protocol`.

## 2. Deliverables
1. `Dockerfile`:
   - Builder stage: `FROM golang:1.27-alpine AS builder` (or `golang:1.24-alpine` if building on GitHub runners, let's check what `.github/workflows/ci.yml` builds—wait: our prompt specifically states: `Multi-stage Dockerfile (FROM golang:1.27-alpine AS builder building static CGO_ENABLED=0 /out/acn-node, runtime stage running as USER 10001:10001 on port 8000)`).
2. `deploy/base/`:
   - `01-deployment.yaml`
   - `02-service.yaml`
   - `03-httproute.yaml` (routing `acn.paulojauregui.com` via `homelab-gateway` in namespace `argocd`)
   - `kustomizeconfig.yaml`
   - `kustomization.yaml`
3. `README.md`:
   - Mermaid architecture diagrams, mathematical formulas ($\omega$, $\nu$, $V_i$), API reference, and verification instructions.

## 3. Verification
```bash
export PATH="/home/tron/.local/go/bin:$PATH"
test -z "$(gofmt -l .)"
go mod verify
go vet ./...
go test -v -race -cover ./...
kubectl kustomize deploy/base
```
