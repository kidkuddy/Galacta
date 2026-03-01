SHELL := /bin/bash
BIN_DIR := bin
GALACTA := $(BIN_DIR)/galacta
JEFF := $(BIN_DIR)/jeff

INSTALL_DIR := /usr/local/bin

.PHONY: all build galacta jeff clean vet test run health bench install uninstall

all: build

# Build both binaries
build: galacta jeff

galacta:
	@mkdir -p $(BIN_DIR)
	go build -o $(GALACTA) ./cmd/galacta/
	@echo "built: $(GALACTA)"

jeff:
	@mkdir -p $(BIN_DIR)
	go build -o $(JEFF) ./cmd/jeff/
	@echo "built: $(JEFF)"

# Run the daemon (requires ANTHROPIC_API_KEY)
run: galacta
	$(GALACTA) --port 9090 --data-dir /tmp/galacta-dev

# Vet all packages
vet:
	go vet ./...

# Run tests (when they exist)
test:
	go test ./...

# Quick health check against a running daemon
health:
	@curl -s http://localhost:9090/health | python3 -m json.tool 2>/dev/null || echo "Galacta not running"

# Run benchmark: Galacta vs Claude Code
bench: build
	@bash bench/bench.sh

# Install binaries to INSTALL_DIR (default: /usr/local/bin)
install: build
	install -m 755 $(GALACTA) $(INSTALL_DIR)/galacta
	install -m 755 $(JEFF) $(INSTALL_DIR)/jeff
	@echo "installed: $(INSTALL_DIR)/galacta $(INSTALL_DIR)/jeff"

# Remove installed binaries
uninstall:
	rm -f $(INSTALL_DIR)/galacta $(INSTALL_DIR)/jeff
	@echo "removed: $(INSTALL_DIR)/galacta $(INSTALL_DIR)/jeff"

# Clean build artifacts
clean:
	rm -f $(GALACTA) $(JEFF)
	rm -rf /tmp/galacta-dev
