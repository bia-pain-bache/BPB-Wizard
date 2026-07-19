VER ?= $(VERSION)
GOOS := $(shell go env GOOS)
GOARCH := $(shell go env GOARCH)
LDFLAGS = -s -w -X "main.VERSION=$(VER)"

GO := GO111MODULE=on CGO_ENABLED=0 go
APP_NAME := BPB-Wizard
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
	GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o "$$outdir/wizard$$ext"; \
	cp LICENSE $$outdir/; \
	archive="$(DIST_DIR)/$(APP_NAME)-$(GOOS)-$(GOARCH)"; \
	if [ "$(GOOS)" = "windows" ] || [ "$(GOOS)" = "darwin" ]; then \
		zip -j -q $$archive.zip $$outdir/*; \
	else \
		tar -czf $$archive.tar.gz -C $$outdir/ .; \
	fi;

clean:
	@rm -rf $(OUT_DIR) $(DIST_DIR)