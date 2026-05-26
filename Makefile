BINARY := bootstrap_environment
DIST   := dist
PKG    := .

# Statically-linked, stripped binaries for distribution.
GOFLAGS := -trimpath -ldflags="-s -w"

TARGETS := \
	linux/amd64 \
	linux/arm64 \
	linux/arm/6 \
	darwin/amd64 \
	darwin/arm64

.PHONY: all build build-all test vet fmt clean

all: build

build:
	go build $(GOFLAGS) -o $(BINARY) $(PKG)

# Cross-compile native binaries for each supported (OS, arch[, GOARM]) triple
# into dist/. Targets formatted as "os/arch" produce "$(BINARY)-os-arch";
# "os/arm/N" produces "$(BINARY)-os-armvN" with GOARM=N.
build-all: $(DIST)
	@for t in $(TARGETS); do \
		os=$$(echo $$t | cut -d/ -f1); \
		arch=$$(echo $$t | cut -d/ -f2); \
		goarm=$$(echo $$t | cut -s -d/ -f3); \
		if [ -n "$$goarm" ]; then \
			suffix=$$arch"v"$$goarm; \
		else \
			suffix=$$arch; \
		fi; \
		out=$(DIST)/$(BINARY)-$$os-$$suffix; \
		echo "==> $$os/$$arch$${goarm:+ GOARM=$$goarm} -> $$out"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch GOARM=$$goarm \
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
