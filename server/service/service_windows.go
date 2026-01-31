// +build windows

package service

import (
	"fmt"
	"runtime"
	"httpserver/server/config"
)

// Install is not supported on Windows
func Install(cfg *config.Config, executablePath string) error {
	return fmt.Errorf("service installation is not supported on %s", runtime.GOOS)
}

// Uninstall is not supported on Windows
func Uninstall() error {
	return fmt.Errorf("service uninstallation is not supported on %s", runtime.GOOS)
}

// IsInstalled always returns false on Windows
func IsInstalled() bool {
	return false
}
