// +build linux

package service

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"text/template"

	"httpserver/server/config"
)

const systemdUnitTemplate = `[Unit]
Description=HTTP Image Hosting Server
After=network.target

[Service]
Type=simple
User={{.User}}
WorkingDirectory={{.WorkingDir}}
ExecStart={{.Executable}} --config {{.ConfigPath}}
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
`

// Install installs the systemd service
func Install(cfg *config.Config, executablePath string) error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("service installation is only supported on Linux")
	}

	// Get config path
	configPath := getConfigPath()

	// Update port in config if needed
	if cfg.Server.Port != 0 {
		if err := config.UpdatePort(configPath, cfg.Server.Port); err != nil {
			return fmt.Errorf("failed to update port in config: %w", err)
		}
	}

	// Get current user
	user := os.Getenv("USER")
	if user == "" {
		user = os.Getenv("USERNAME")
	}

	// Get executable directory
	execDir := filepath.Dir(executablePath)
	if execDir == "." {
		if cwd, err := os.Getwd(); err == nil {
			execDir = cwd
		}
	}

	// Prepare template data
	data := struct {
		User        string
		WorkingDir  string
		Executable  string
		ConfigPath  string
	}{
		User:       user,
		WorkingDir: execDir,
		Executable: getAbsolutePath(executablePath),
		ConfigPath: configPath,
	}

	// Generate unit file
	tmpl, err := template.New("service").Parse(systemdUnitTemplate)
	if err != nil {
		return fmt.Errorf("failed to parse template: %w", err)
	}

	unitPath := "/etc/systemd/system/httpserver.service"
	unitFile, err := os.Create(unitPath)
	if err != nil {
		return fmt.Errorf("failed to create unit file: %w", err)
	}
	defer unitFile.Close()

	if err := tmpl.Execute(unitFile, data); err != nil {
		return fmt.Errorf("failed to execute template: %w", err)
	}

	// Run systemctl commands
	log.Println("Reloading systemd daemon...")
	runSystemctl("daemon-reload")

	log.Println("Enabling httpserver service...")
	runSystemctl("enable", "httpserver")

	log.Println("Starting httpserver service...")
	runSystemctl("start", "httpserver")

	log.Printf("Service installed and started successfully!")
	log.Printf("Use 'systemctl status httpserver' to check service status")
	log.Printf("Use 'journalctl -u httpserver -f' to view logs")

	return nil
}

// Uninstall uninstalls the systemd service
func Uninstall() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("service uninstallation is only supported on Linux")
	}

	unitPath := "/etc/systemd/system/httpserver.service"

	// Check if service exists
	if _, err := os.Stat(unitPath); os.IsNotExist(err) {
		return fmt.Errorf("service is not installed")
	}

	log.Println("Stopping httpserver service...")
	runSystemctl("stop", "httpserver")

	log.Println("Disabling httpserver service...")
	runSystemctl("disable", "httpserver")

	log.Println("Removing unit file...")
	if err := os.Remove(unitPath); err != nil {
		return fmt.Errorf("failed to remove unit file: %w", err)
	}

	log.Println("Reloading systemd daemon...")
	runSystemctl("daemon-reload")

	log.Println("Service uninstalled successfully")
	log.Println("Config and data files were preserved")

	return nil
}

// IsInstalled checks if the service is installed
func IsInstalled() bool {
	if runtime.GOOS != "linux" {
		return false
	}

	_, err := os.Stat("/etc/systemd/system/httpserver.service")
	return err == nil
}

// runSystemctl executes a systemctl command
func runSystemctl(args ...string) error {
	cmd := exec.Command("systemctl", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// getAbsolutePath returns the absolute path of a file
func getAbsolutePath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

// getConfigPath returns the config file path
func getConfigPath() string {
	if path := os.Getenv("HTTPSERVER_CONFIG"); path != "" {
		return path
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "./HttpServer/config.json"
	}

	return filepath.Join(home, "HttpServer", "config.json")
}
