BINARY := bootstrap_environment
DIST   := dist
PKG    := .

# Statically-linked, stripped binaries for distribution.
GOFLAGS := -trimpath -ldflags="-s -w"

TARGETS := \
	linux/amd64 \
	linux/arm64 \
	darwin/amd64 \
	darwin/arm64

.PHONY: all build build-all test vet fmt clean

all: build

build:
	go build $(GOFLAGS) -o $(BINARY) $(PKG)

# Cross-compile native binaries for each supported (OS, arch) pair into dist/.
build-all: $(DIST)
	@for t in $(TARGETS); do \
		os=$${t%/*}; arch=$${t#*/}; \
		out=$(DIST)/$(BINARY)-$$os-$$arch; \
		echo "==> $$os/$$arch -> $$out"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch \
			go build $(GOFLAGS) -o $$out $(PKG) || exit 1; \
	done

$(DIST):
	mkdir -p $(DIST)

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

clean:
	rm -rf $(DIST) $(BINARY)
