HOSTNAME    := registry.terraform.io
NAMESPACE   := Sherman-Studio
NAME        := revolut
BINARY      := terraform-provider-$(NAME)
VERSION     := 0.1.0
OS_ARCH     := $(shell go env GOOS)_$(shell go env GOARCH)

.PHONY: build install test testacc lint fmt fmt-check vet docs docs-check build-snapshot release tidy

build:
	go build -o $(BINARY) .

# Install into the Terraform CLI filesystem mirror for local testing.
install: build
	mkdir -p ~/.terraform.d/plugins/$(HOSTNAME)/$(NAMESPACE)/$(NAME)/$(VERSION)/$(OS_ARCH)
	mv $(BINARY) ~/.terraform.d/plugins/$(HOSTNAME)/$(NAMESPACE)/$(NAME)/$(VERSION)/$(OS_ARCH)

test:
	go test -cover ./...

# Acceptance tests hit the real Revolut API; requires REVOLUT_API_KEY (sandbox).
testacc:
	TF_ACC=1 go test -v -timeout 120m ./...

lint:
	golangci-lint run

fmt:
	gofmt -w .

fmt-check:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then echo "Not gofmt-ed:"; echo "$$unformatted"; exit 1; fi

vet:
	go vet ./...

docs:
	go generate ./...

docs-check: docs
	@if [ -n "$$(git status --porcelain docs/)" ]; then \
	  echo "docs/ is stale — run 'make docs' and commit."; git diff docs/; exit 1; fi

build-snapshot:
	goreleaser build --snapshot --clean

release:
	goreleaser release --clean

tidy:
	go mod tidy
