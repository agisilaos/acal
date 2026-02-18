.PHONY: build test vet fmt docs-check release-check release

build:
	go build -o acal ./cmd/acal

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w cmd/acal/*.go internal/**/*.go

docs-check:
	./scripts/docs-check.sh

release-check:
	@if [ -z "$(VERSION)" ]; then echo "VERSION is required (e.g. make release-check VERSION=v0.1.0)"; exit 2; fi
	./scripts/release-check.sh "$(VERSION)"

release:
	@if [ -z "$(VERSION)" ]; then echo "VERSION is required (e.g. make release VERSION=v0.1.0)"; exit 2; fi
	./scripts/release.sh "$(VERSION)"
