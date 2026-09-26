STATICCHECK_VERSION := 2026.1
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

export CGO_ENABLED := 0

.PHONY: build test lint bench fuzz check-static clean

build:
	go build -trimpath -ldflags '$(LDFLAGS)' -o bin/loc ./cmd/loc

test:
	go test ./...

lint:
	go vet ./...
	go run honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION) ./...

bench:
	go test -run '^$$' -bench . -benchmem ./...

FUZZTIME ?= 60s
fuzz:
	go test ./internal/scrub -run '^$$' -fuzz FuzzScrub -fuzztime $(FUZZTIME)

# Fails unless bin/loc is a statically linked binary (core principle P4).
check-static: build
	@file bin/loc | grep -q 'statically linked' || { file bin/loc; echo 'bin/loc is not statically linked'; exit 1; }
	@echo 'bin/loc is statically linked'

clean:
	rm -rf bin
