package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"httpserver/server/cleanup"
	"httpserver/server/config"
	"httpserver/server/db"
	"httpserver/server/httpd"
	"httpserver/server/service"
)

var (
	version = "1.0.0"
)

func main() {
	// Parse command line arguments
	args := os.Args[1:]

	// Check for subcommands (set, get, start)
	if len(args) > 0 {
		switch args[0] {
		case "set":
			handleSetCommand(args)
			return
		case "get":
			handleGetCommand(args)
			return
		case "start":
			// Remove "start" from args and continue to server start
			args = args[1:]
			os.Args = append([]string{os.Args[0]}, args...)
		}
	}

	// Define command line flags (for start command)
	flagInstall := flag.Bool("i", false, "Install as systemd service (Linux only)")
	flagUninstall := flag.Bool("u", false, "Uninstall systemd service (Linux only)")
	flagPort := flag.Int("p", 0, "Port to listen on (overrides config)")
	flagConfig := flag.String("c", "", "Path to database file")
	flagNoRestart := flag.Bool("no-restart", false, "Disable auto restart (ignored on Windows)")
	flagVersion := flag.Bool("v", false, "Show version information")
	flagHelp := flag.Bool("h", false, "Show help information")

	flag.Parse()

	// Suppress unused warning (used in service installation)
	_ = flagNoRestart

	// Show version
	if *flagVersion {
		fmt.Printf("HTTP Image Hosting Server v%s\n", version)
		fmt.Printf("Built for %s/%s\n", runtime.GOOS, runtime.GOARCH)
		return
	}

	// Show help
	if *flagHelp {
		printHelp()
		return
	}

	// Handle service uninstall
	if *flagUninstall {
		if err := service.Uninstall(); err != nil {
			log.Fatalf("Failed to uninstall service: %v", err)
		}
		return
	}

	// Determine database path
	dbPath := *flagConfig
	if dbPath == "" {
		dbPath = getDefaultDBPath()
	}

	// Open database (must be opened first to get config)
	database, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	// Build config from database
	cfg := buildConfigFromDB(database)

	// Override port from command line
	if *flagPort > 0 {
		cfg.Server.Port = *flagPort
		// Save to database for persistence
		if err := database.SetConfig("server.port", fmt.Sprintf("%d", *flagPort)); err != nil {
			log.Printf("Warning: failed to save port to database: %v", err)
		}
	}

	// Handle service install
	if *flagInstall {
		// Get executable path
		execPath, err := os.Executable()
		if err != nil {
			log.Fatalf("Failed to get executable path: %v", err)
		}

		// Build config with port for service
		serviceCfg := cfg
		if *flagPort > 0 {
			serviceCfg.Server.Port = *flagPort
		}

		if err := service.Install(serviceCfg, execPath); err != nil {
			log.Fatalf("Failed to install service: %v", err)
		}
		return
	}

	// Ensure directories exist
	if err := config.EnsureDirectories(cfg); err != nil {
		log.Fatalf("Failed to create directories: %v", err)
	}

	// Start cleanup manager
	cleanupMgr := cleanup.NewCleanupManager(&cleanup.Config{
		ImagesDir:       cfg.Storage.ImagesDir,
		CleanupInterval: cfg.Storage.CleanupInterval,
	}, database)
	cleanupMgr.Start()
	defer cleanupMgr.Stop()

	// Create and start HTTP server
	server := httpd.NewServer(cfg, database)

	// Handle shutdown gracefully
	go handleShutdown(server, cleanupMgr)

	// Start server
	if err := server.Start(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

func handleSetCommand(args []string) {
	if len(args) < 3 {
		fmt.Fprintln(os.Stderr, "Error: 'set' command requires key and value")
		fmt.Fprintln(os.Stderr, "Usage: httpserver set <key> <value>")
		os.Exit(1)
	}

	key := args[1]
	value := strings.Join(args[2:], " ")

	// Determine database path
	dbPath := getDefaultDBPath()

	// Open database
	database, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	// Set config value
	if err := database.SetConfig(key, value); err != nil {
		log.Fatalf("Failed to set config: %v", err)
	}

	fmt.Printf("Config updated: %s = %s\n", key, value)
}

func handleGetCommand(args []string) {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "Error: 'get' command requires a key or 'all'")
		fmt.Fprintln(os.Stderr, "Usage: httpserver get <key> | all")
		os.Exit(1)
	}

	// Determine database path
	dbPath := getDefaultDBPath()

	// Open database
	database, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	key := args[1]

	if key == "all" {
		// Show all configuration
		allConfig := database.GetAllConfig()
		fmt.Println("Configuration:")
		fmt.Println("================")
		// Group by prefix
		groups := make(map[string][]string)
		for k := range allConfig {
			prefix := strings.Split(k, ".")[0]
			groups[prefix] = append(groups[prefix], k)
		}
		// Print in order: server, storage, auth, security
		order := []string{"server", "storage", "auth", "security"}
		for _, prefix := range order {
			if keys, ok := groups[prefix]; ok {
				fmt.Printf("\n[%s]\n", strings.ToUpper(prefix))
				for _, k := range keys {
					fmt.Printf("  %s: %s\n", k, allConfig[k])
				}
			}
		}
	} else {
		// Get single value
		value := database.GetConfig(key)
		if value == "" {
			fmt.Printf("Config key '%s' not found or empty\n", key)
			os.Exit(1)
		}
		fmt.Println(value)
	}
}

func buildConfigFromDB(database *db.Database) *config.Config {
	cfg := &config.Config{}

	// Server config
	cfg.Server.Host = database.GetConfig("server.host")
	cfg.Server.Port = database.GetConfigInt("server.port")

	// Storage config
	cfg.Storage.ImagesDir = database.GetConfig("storage.images_dir")
	cfg.Storage.MaxFileSize = int64(database.GetConfigInt("storage.max_file_size"))
	cfg.Storage.CleanupInterval = database.GetConfigInt("storage.cleanup_interval")
	cfg.Storage.DefaultTTL = database.GetConfigInt("storage.default_ttl")
	cfg.Storage.MaxTTL = database.GetConfigInt("storage.max_ttl")

	// Auth config
	cfg.Auth.APIKey = database.GetConfig("auth.api_key")
	cfg.Auth.AdminUsername = database.GetConfig("auth.admin_username")
	cfg.Auth.AdminPassword = database.GetConfig("auth.admin_password")
	cfg.Auth.ListPassword = database.GetConfig("auth.list_password")

	// Security config
	// IP whitelist is stored as comma-separated string
	ipWhitelistStr := database.GetConfig("security.ip_whitelist")
	if ipWhitelistStr != "" {
		cfg.Security.IPWhitelist = strings.Split(ipWhitelistStr, ",")
	} else {
		cfg.Security.IPWhitelist = []string{}
	}
	cfg.Security.RateLimitPerMinute = database.GetConfigInt("security.rate_limit_per_minute")
	cfg.Security.SessionTimeout = database.GetConfigInt("security.session_timeout")

	// Database config
	cfg.Database.Path = database.GetConfig("database.path")
	if cfg.Database.Path == "" {
		cfg.Database.Path = getDefaultDBPath()
	}

	// Auto restart config
	autoRestartStr := database.GetConfig("auto_restart.enabled")
	cfg.AutoRestart.Enabled = autoRestartStr == "true"
	cfg.AutoRestart.MaxRestartCount = database.GetConfigInt("auto_restart.max_restart_count")

	return cfg
}

func printHelp() {
	fmt.Printf("HTTP Image Hosting Server v%s\n\n", version)
	fmt.Println("Usage:")
	fmt.Println("  httpserver [command] [options]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  start              Start the server (default)")
	fmt.Println("  set <key> <value>  Set configuration value")
	fmt.Println("  get <key>          Get configuration value")
	fmt.Println("  get all            Show all configuration")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  -i                 Install as systemd service (Linux only)")
	fmt.Println("  -u                 Uninstall systemd service (Linux only)")
	fmt.Println("  -p <port>          Port to listen on (overrides config)")
	fmt.Println("  -c <path>          Path to database file")
	fmt.Println("  --no-restart       Disable auto restart (Linux only)")
	fmt.Println("  -v, --version      Show version information")
	fmt.Println("  -h, --help         Show this help message")
	fmt.Println()
	fmt.Println("Configuration Keys:")
	fmt.Println("  server.host                    Server host address")
	fmt.Println("  server.port                    Server port")
	fmt.Println("  storage.images_dir             Images storage directory")
	fmt.Println("  storage.max_file_size          Max file size in bytes")
	fmt.Println("  storage.cleanup_interval       Cleanup interval in minutes")
	fmt.Println("  storage.default_ttl            Default TTL in hours")
	fmt.Println("  storage.max_ttl                Maximum TTL in hours")
	fmt.Println("  auth.api_key                   API key for upload/delete")
	fmt.Println("  auth.admin_username            Admin username")
	fmt.Println("  auth.admin_password            Admin password")
	fmt.Println("  auth.list_password             File list password")
	fmt.Println("  security.ip_whitelist          Comma-separated IP whitelist")
	fmt.Println("  security.rate_limit_per_minute Rate limit per IP")
	fmt.Println("  security.session_timeout       Session timeout in seconds")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  httpserver                    # Start server")
	fmt.Println("  httpserver -p 80              # Start server on port 80")
	fmt.Println("  httpserver set server.port 4900     # Set port to 4900")
	fmt.Println("  httpserver get server.port          # Get port value")
	fmt.Println("  httpserver get all                 # Show all config")
	fmt.Println("  httpserver -p 8080 -i         # Install service on port 8080")
	fmt.Println("  httpserver -u                 # Uninstall service")
}

func getDefaultDBPath() string {
	if runtime.GOOS == "windows" {
		// Windows: use executable directory
		if exePath, err := os.Executable(); err == nil {
			return filepath.Join(filepath.Dir(exePath), "metadata.db")
		}
		return "metadata.db"
	}

	// Linux: use ~/HttpServer
	home, err := os.UserHomeDir()
	if err != nil {
		return "./HttpServer/metadata.db"
	}
	return filepath.Join(home, "HttpServer", "metadata.db")
}

func handleShutdown(server *httpd.Server, cleanupMgr *cleanup.CleanupManager) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	<-sigChan
	log.Println("Shutting down...")

	// Note: Server doesn't have explicit Shutdown method in this simple implementation
	// In production, you'd want to implement graceful shutdown
	cleanupMgr.Stop()

	os.Exit(0)
}
