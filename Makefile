STATICCHECK_VERSION := 2026.1
# Only v* tags are loxx versions; other tags (e.g. model-minilm-l6-v2-f16, the
# model weights release) must not become the version string.
VERSION ?= $(shell git describe --tags --match 'v[0-9]*' --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

export CGO_ENABLED := 0

# Linux has sha256sum; macOS has shasum instead (same output format).
SHA256SUM := $(shell command -v sha256sum >/dev/null 2>&1 && echo sha256sum || echo 'shasum -a 256')

# The embedding model's weights are compiled into the binary but not stored in
# git: `make model` downloads them from a GitHub Release and verifies the
# SHA-256. Reproduce the file from the original Hugging Face source with
# tools/model/convert.py (see internal/embed/model/README.md).
MODEL_FILE := internal/embed/model/minilm-l6-v2.f16.safetensors
MODEL_SHA256 := aa3d97aea538b3247506fd426683a526d23ed345ac2c20ea3172296f57ea272b
MODEL_URL := https://github.com/AniketR10/loxx/releases/download/model-minilm-l6-v2-f16/minilm-l6-v2.f16.safetensors

.PHONY: model build dist test lint bench fuzz check-static clean

model:
	@if [ -f $(MODEL_FILE) ] && echo "$(MODEL_SHA256)  $(MODEL_FILE)" | $(SHA256SUM) -c --status; then \
		echo 'model: present, SHA-256 verified'; \
	else \
		echo 'model: downloading $(MODEL_URL)'; \
		curl -fsSL --retry 3 -o $(MODEL_FILE).tmp $(MODEL_URL) \
			&& echo "$(MODEL_SHA256)  $(MODEL_FILE).tmp" | $(SHA256SUM) -c --status \
			|| { rm -f $(MODEL_FILE).tmp; echo 'model: download failed or SHA-256 mismatch'; exit 1; }; \
		mv $(MODEL_FILE).tmp $(MODEL_FILE); \
		echo 'model: downloaded, SHA-256 verified'; \
	fi

build: model
	go build -trimpath -ldflags '$(LDFLAGS)' -o bin/loxx ./cmd/loxx

# Release assets, named as install.sh expects: a binary for each system and
# architecture (loxx-<os>-<arch>), the installer, and checksums for all of them.
DIST_TARGETS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64
dist: model
	rm -rf dist && mkdir -p dist
	for target in $(DIST_TARGETS); do \
		os=$${target%/*} arch=$${target#*/}; \
		GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags '$(LDFLAGS)' -o dist/loxx-$$os-$$arch ./cmd/loxx || exit 1; \
		sh tools/checkstatic.sh dist/loxx-$$os-$$arch || exit 1; \
	done
	cp install.sh dist/install.sh
	cd dist && $(SHA256SUM) loxx-* install.sh >checksums.txt

test: model
	go test ./...

lint: model
	go vet ./...
	go run honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION) ./...

bench: model
	go test -run '^$$' -bench . -benchmem ./...

FUZZTIME ?= 60s
fuzz:
	go test ./internal/scrub -run '^$$' -fuzz FuzzScrub -fuzztime $(FUZZTIME)

# Fails unless bin/loxx is one self-contained file (core principle P4; see
# tools/checkstatic.sh for what that means on macOS).
check-static: build
	@sh tools/checkstatic.sh bin/loxx

clean:
	rm -rf bin dist
