package naming

import (
	"crypto/rand"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// GenerateFileName generates a new filename based on the naming rule
// Format: YYYYMMDD-HHMMSSmmm-random16bytes.ext
func GenerateFileName(originalName string) string {
	// Get current time with milliseconds
	now := time.Now()

	// Format: YYYYMMDD-HHMMSSmmm-random16bytes.ext
	timestamp := now.Format("20060102-150405")
	milliseconds := now.Nanosecond() / 1000000
	timestampWithMs := fmt.Sprintf("%s%03d", timestamp, milliseconds)

	// Generate 16 random bytes (32 hex characters)
	randomBytes := make([]byte, 16)
	if _, err := rand.Read(randomBytes); err != nil {
		// Fallback: use timestamp-based random if crypto rand fails
		randomBytes = []byte(fmt.Sprintf("%016x", now.UnixNano()))
	}
	randomStr := fmt.Sprintf("%032x", randomBytes)

	// Get file extension
	ext := strings.ToLower(filepath.Ext(originalName))
	if ext == "" {
		ext = ".bin"
	}

	return fmt.Sprintf("%s-%s%s", timestampWithMs, randomStr, ext)
}

// GenerateDateDir generates the date directory name (YYYYMMDD)
func GenerateDateDir() string {
	return time.Now().Format("20060102")
}

// GenerateFilePath generates the full relative file path
// Returns: YYYYMMDD/YYYYMMDD-HHMMSSmmm-random16bytes.ext
func GenerateFilePath(originalName string) (string, error) {
	date := GenerateDateDir()
	fileName := GenerateFileName(originalName)
	return filepath.Join(date, fileName), nil
}

// ParseDateFromPath extracts the date directory from a file path
func ParseDateFromPath(filePath string) string {
	// Normalize path separators to /
	filePath = filepath.ToSlash(filePath)
	parts := strings.Split(filePath, "/")
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}

// GetStoragePath returns the full storage path for a relative file path
func GetStoragePath(imagesDir, relativePath string) string {
	return filepath.Join(imagesDir, relativePath)
}
