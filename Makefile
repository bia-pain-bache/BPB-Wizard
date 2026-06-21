VER ?= $(VERSION)
GOOS := $(shell go env GOOS)
GOARCH := $(shell go env GOARCH)

LDFLAGS = -s -w \
	-X "main.BuildTimestamp=$(shell date -u '+%Y-%m-%d %H:%M:%S')" \
	-X "main.VERSION=$(VER)" \
	-X "main.goVersion=$(shell go version | sed -r 's/go version go(.*)\ .*/\1/')"

GO := GO111MODULE=on CGO_ENABLED=0 go
GOLANGCI_LINT_VERSION = v1.61.0
APP_NAME := Che-Wizard
OUT_DIR := bin
DIST_DIR := dist

.PHONY: build clean

build:
	@mkdir -p $(OUT_DIR) $(DIST_DIR); \
	if [ "$(GOOS)" = "windows" ]; then \
		ext=".exe"; \
	else \
		ext=""; \
	fi; \
	echo "Building for $(GOOS)-$(GOARCH)..."; \
	outdir="$(OUT_DIR)/$(APP_NAME)-$(GOOS)-$(GOARCH)"; \
	GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o "$$outdir/Wizard$$ext"; \
	cp LICENSE $$outdir/; \
	archive="$(DIST_DIR)/$(APP_NAME)-$(GOOS)-$(GOARCH)"; \
	if [ "$(GOOS)" = "windows" ] || [ "$(GOOS)" = "darwin" ]; then \
		zip -j -q $$archive.zip $$outdir/*; \
	else \
		tar -czf $$archive.tar.gz -C $$outdir/ .; \
	fi;

clean:
	@rm -rf $(OUT_DIR) $(DIST_DIR)