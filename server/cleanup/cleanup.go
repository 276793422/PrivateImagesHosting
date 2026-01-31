package cleanup

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"httpserver/server/db"
	"httpserver/server/naming"
)

// CleanupManager handles file cleanup operations
type CleanupManager struct {
	cfg            *Config
	db             *db.Database
	stopChan       chan struct{}
}

type Config struct {
	ImagesDir       string
	CleanupInterval int // minutes
}

// NewCleanupManager creates a new cleanup manager
func NewCleanupManager(cfg *Config, database *db.Database) *CleanupManager {
	return &CleanupManager{
		cfg:      cfg,
		db:       database,
		stopChan: make(chan struct{}),
	}
}

// Start starts the cleanup manager
func (cm *CleanupManager) Start() {
	interval := time.Duration(cm.cfg.CleanupInterval) * time.Minute
	ticker := time.NewTicker(interval)

	log.Printf("Cleanup manager started (interval: %v)", interval)

	// Run initial cleanup
	go cm.runCleanup()

	// Run periodic cleanup
	go func() {
		for {
			select {
			case <-ticker.C:
				cm.runCleanup()
			case <-cm.stopChan:
				ticker.Stop()
				return
			}
		}
	}()
}

// Stop stops the cleanup manager
func (cm *CleanupManager) Stop() {
	close(cm.stopChan)
}

// runCleanup executes the cleanup process
func (cm *CleanupManager) runCleanup() {
	log.Println("Starting cleanup process...")

	// Get expired files
	expiredFiles, err := cm.db.GetExpiredFiles()
	if err != nil {
		log.Printf("Error getting expired files: %v", err)
		return
	}

	if len(expiredFiles) == 0 {
		log.Println("No expired files to clean up")
		return
	}

	deletedCount := 0
	freedSpace := int64(0)

	for _, file := range expiredFiles {
		// Delete physical file
		fullPath := naming.GetStoragePath(cm.cfg.ImagesDir, file.FilePath)
		if err := os.Remove(fullPath); err != nil {
			if !os.IsNotExist(err) {
				log.Printf("Error deleting file %s: %v", file.FilePath, err)
			}
			// Still remove from database if file doesn't exist
		} else {
			deletedCount++
			freedSpace += file.FileSize
		}

		// Delete metadata from database
		if err := cm.db.DeleteFileMetadata(file.FilePath); err != nil {
			log.Printf("Error deleting metadata for %s: %v", file.FilePath, err)
		} else {
			log.Printf("Deleted expired file: %s (original: %s, size: %d bytes)",
				file.FilePath, file.OriginalName, file.FileSize)
		}

		// Try to remove empty date directory
		dateDir := naming.ParseDateFromPath(file.FilePath)
		if dateDir != "" {
			fullDirPath := filepath.Join(cm.cfg.ImagesDir, dateDir)
			if err := removeEmptyDir(fullDirPath); err != nil {
				log.Printf("Note: could not remove directory %s: %v", dateDir, err)
			}
		}
	}

	log.Printf("Cleanup complete: deleted %d files, freed %s", deletedCount, formatBytes(freedSpace))
}

// removeEmptyDir removes a directory if it's empty
func removeEmptyDir(dirPath string) error {
	// Check if directory is empty
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return err
	}

	if len(entries) == 0 {
		return os.Remove(dirPath)
	}

	return nil
}

// formatBytes formats bytes to human readable string
func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return "0 B"
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// RunOnce runs cleanup once (for manual trigger)
func (cm *CleanupManager) RunOnce() {
	cm.runCleanup()
}
