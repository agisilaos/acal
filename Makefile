.PHONY: build test vet fmt fmt-check docs-check release-check release release-dry-run

build:
	go build -o acal ./cmd/acal

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w cmd/acal/*.go internal/**/*.go

fmt-check:
	@test -z "$$(gofmt -l cmd internal)"

docs-check:
	./scripts/docs-check.sh

release-check:
	@if [ -z "$(VERSION)" ]; then echo "VERSION is required (e.g. make release-check VERSION=v0.1.0)"; exit 2; fi
	./scripts/release-check.sh "$(VERSION)"

release:
	@if [ -z "$(VERSION)" ]; then echo "VERSION is required (e.g. make release VERSION=v0.1.0)"; exit 2; fi
	./scripts/release.sh "$(VERSION)"

release-dry-run:
	@if [ -z "$(VERSION)" ]; then echo "VERSION is required (e.g. make release-dry-run VERSION=v0.1.0)"; exit 2; fi
	./scripts/release.sh "$(VERSION)" --dry-run
