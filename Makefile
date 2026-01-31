.PHONY: all server client clean build-linux build-windows help

# Build variables
BINARY_SERVER = httpserver
BINARY_CLIENT = http-cli
VERSION = 1.0.0
LDFLAGS = -ldflags "-X main.version=$(VERSION)"

# Default target
all: server client

# Build server for current platform
server:
	@echo "Building server for $(GOOS)/$(GOARCH)..."
	go build $(LDFLAGS) -o $(BINARY_SERVER) ./server

# Build client for current platform
client:
	@echo "Building client for $(GOOS)/$(GOARCH)..."
	go build $(LDFLAGS) -o $(BINARY_CLIENT) ./client

# Build for Linux (amd64)
build-linux-amd64:
	@echo "Building for Linux amd64..."
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BINARY_SERVER)-linux-amd64 ./server
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BINARY_CLIENT)-linux-amd64 ./client

# Build for Linux (arm64)
build-linux-arm64:
	@echo "Building for Linux arm64..."
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o $(BINARY_SERVER)-linux-arm64 ./server
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o $(BINARY_CLIENT)-linux-arm64 ./client

# Build for Windows (amd64)
build-windows-amd64:
	@echo "Building for Windows amd64..."
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(BINARY_SERVER).exe ./server
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(BINARY_CLIENT).exe ./client

# Build all platforms
build-all: build-linux-amd64 build-linux-arm64 build-windows-amd64

# Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -f $(BINARY_SERVER)
	rm -f $(BINARY_CLIENT)
	rm -f $(BINARY_SERVER)-*
	rm -f $(BINARY_CLIENT)-*
	rm -f $(BINARY_SERVER).exe
	rm -f $(BINARY_CLIENT).exe

# Download dependencies
deps:
	@echo "Downloading dependencies..."
	go mod download
	go mod tidy

# Run tests
test:
	@echo "Running tests..."
	go test -v ./...

# Help
help:
	@echo "Available targets:"
	@echo "  all              - Build server and client for current platform"
	@echo "  server           - Build server for current platform"
	@echo "  client           - Build client for current platform"
	@echo "  build-linux-amd64    - Build for Linux amd64"
	@echo "  build-linux-arm64    - Build for Linux arm64"
	@echo "  build-windows-amd64  - Build for Windows amd64"
	@echo "  build-all        - Build for all platforms"
	@echo "  clean            - Remove build artifacts"
	@echo "  deps             - Download and tidy dependencies"
	@echo "  test             - Run tests"
	@echo "  help             - Show this help message"
