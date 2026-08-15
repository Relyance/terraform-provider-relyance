GOLANGCI_LINT_VERSION ?= v2.12.2

default: build

# Compile all packages.
build:
	go build ./...

# Install the provider binary into GOBIN (for dev_overrides).
install:
	go install ./cmd/terraform-provider-relyance

# Unit tests (no network).
test:
	go test ./... -count=1

# Acceptance tests — creates/destroys real resources; requires RELYANCE_* env + a non-prod tenant.
testacc:
	TF_ACC=1 go test ./... -count=1 -timeout 30m

# Static analysis.
vet:
	go vet ./...

lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run ./...

# Vulnerability scan.
vulncheck:
	go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...

# Format.
fmt:
	gofmt -s -w .

# Regenerate provider docs from schema + examples.
docs:
	./scripts/gendocs.sh

# Regenerate the vendored integrations-api client (internal/apiclient) from its
# OpenAPI spec. Requires Docker (openapi-generator-cli).
generate:
	./scripts/generate-apiclient.sh

# Tidy module deps (main + tools).
tidy:
	go mod tidy
	cd tools && go mod tidy

.PHONY: default build install test testacc vet lint vulncheck fmt docs generate tidy
