BINARY_DIR = bin
RPS_BINARY = $(BINARY_DIR)/rps
RPC_BINARY = $(BINARY_DIR)/rpc

LDFLAGS := -s -w

.PHONY: all build clean rps rpc install

all: build

build: rps rpc

$(BINARY_DIR):
	mkdir -p $(BINARY_DIR)

rps: $(BINARY_DIR)
	go build -v -trimpath -ldflags "$(LDFLAGS)" -o $(RPS_BINARY) ./cmd/rps

rpc: $(BINARY_DIR)
	go build -v -trimpath -ldflags "$(LDFLAGS)" -o $(RPC_BINARY) ./cmd/rpc

clean:
	rm -rf $(BINARY_DIR)

install: build
	sudo install -m 755 $(RPS_BINARY) /usr/local/bin/
	sudo install -m 755 $(RPC_BINARY) /usr/local/bin/

test:
	go test ./...

.PHONY: help
help:
	@echo "Available targets:"
	@echo "  build    - Build both rps and rpc binaries"
	@echo "  rps      - Build only the reverse proxy server"
	@echo "  rpc      - Build only the reverse proxy client"
	@echo "  clean    - Remove build artifacts"
	@echo "  install  - Install binaries to /usr/local/bin"
	@echo "  test     - Run tests"
	@echo "  help     - Show this help message"
