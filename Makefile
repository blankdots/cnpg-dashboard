help:
	@echo 'CNPG Dashboard'
	@echo ''
	@echo 'Targets:'
	@echo '  build     - Build the dashboard binary'
	@echo '  run       - Run the dashboard locally'
	@echo '  frontend  - Build the frontend (requires npm)'
	@echo '  kind-create - Create kind cluster for local dev'
	@echo '  kind-delete - Delete kind cluster'
	@echo '  tilt-up   - Start Tilt dev loop'
	@echo '  tilt-down - Stop Tilt'
	@echo '  test      - Run unit tests'
	@echo '  lint      - Run golangci-lint'

build:
	@go build -o dashboard ./cmd/dashboard

run: build
	@STATIC_DIR=./static ./dashboard

frontend:
	@cd frontend && npm install && npm run build
	@mkdir -p static && cp -r frontend/dist/* static/

# --- kind + Tilt (local dev) ---
kind-create:
	@kind create cluster --config dev/kind.yaml

kind-delete:
	@kind delete cluster --name cnpg-dashboard

# Install CloudNativePG operator (required for dashboard; Tilt does this automatically)
cnpg-install:
	@helm repo add cnpg https://cloudnative-pg.github.io/charts 2>/dev/null || true
	@helm upgrade --install cnpg cnpg/cloudnative-pg -n cnpg-system --create-namespace --wait

tilt-up:
	@tilt up

tilt-down:
	@tilt down

test:
	@go test -v -coverprofile=coverage.txt -covermode=atomic ./...

lint:
	@if ! command -v golangci-lint >/dev/null; then \
		echo "Golangci-lint needs to be installed."; \
		exit 1; \
	fi
	@golangci-lint run -E bodyclose,gocritic,gofmt,gosec,govet,nestif,nlreturn,revive,rowserrcheck
