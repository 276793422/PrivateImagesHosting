package db

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Database handles all file metadata operations using JSON storage
type Database struct {
	filePath   string
	data       *DatabaseData
	mux        sync.RWMutex
	autoSave   chan struct{}
}

// DatabaseData represents the complete database structure
type DatabaseData struct {
	Files       map[int64]*FileMetadata `json:"files"`
	NextID      int64                   `json:"next_id"`
	Config      map[string]string        `json:"config"`
}

// FileMetadata represents metadata for a stored file
type FileMetadata struct {
	ID           int64     `json:"id"`
	FileName     string    `json:"file_name"`      // Generated filename
	OriginalName string    `json:"original_name"`  // Original filename
	FilePath     string    `json:"file_path"`      // Relative path from Images root
	FileSize     int64     `json:"file_size"`
	UploadedAt   time.Time `json:"uploaded_at"`
	ExpiresAt    time.Time `json:"expires_at"`
	TTL          int       `json:"ttl"`
	RemoteIP     string    `json:"remote_ip"`
}

var globalDB *Database

// Default configuration values
const (
	defaultServerHost   = "0.0.0.0"
	defaultServerPort   = 8080
	defaultImagesDir    = "./Images"
	defaultMaxFileSize  = 100 * 1024 * 1024 // 100MB
	defaultCleanupInterval = 60
	defaultDefaultTTL    = 1
	defaultMaxTTL        = 8760 // 365 days
	defaultAPIKey       = "change-me-api-key"
	defaultAdminUser     = "276793422"
	defaultAdminPass     = "490003219"
	defaultListPass      = "490003219"
	defaultIPWhitelist   = ""
	defaultRateLimit    = 60
	defaultSessionTimeout = 300
)

// Open opens the database connection and initializes storage
func Open(dbPath string) (*Database, error) {
	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	database := &Database{
		filePath: dbPath,
		data: &DatabaseData{
			Files:  make(map[int64]*FileMetadata),
			NextID: 1,
			Config: make(map[string]string),
		},
		autoSave: make(chan struct{}, 1),
	}

	// Load existing data if file exists
	if _, err := os.Stat(dbPath); err == nil {
		data, err := os.ReadFile(dbPath)
		if err == nil {
			if err := json.Unmarshal(data, database.data); err == nil {
				// Successfully loaded
			}
		}
	}

	// Initialize default config if not exists
	if len(database.data.Config) == 0 {
		database.initDefaultConfig()
	}

	// Start auto-save goroutine
	go database.autoSaveLoop()

	globalDB = database
	return database, nil
}

// initDefaultConfig initializes default configuration values
func (d *Database) initDefaultConfig() {
	d.data.Config = map[string]string{
		"server.host":                  defaultServerHost,
		"server.port":                  strconv.Itoa(defaultServerPort),
		"storage.images_dir":           defaultImagesDir,
		"storage.max_file_size":         strconv.FormatInt(defaultMaxFileSize, 10),
		"storage.cleanup_interval":      strconv.Itoa(defaultCleanupInterval),
		"storage.default_ttl":           strconv.Itoa(defaultDefaultTTL),
		"storage.max_ttl":               strconv.Itoa(defaultMaxTTL),
		"auth.api_key":                 defaultAPIKey,
		"auth.admin_username":           defaultAdminUser,
		"auth.admin_password":           defaultAdminPass,
		"auth.list_password":            defaultListPass,
		"security.ip_whitelist":         defaultIPWhitelist,
		"security.rate_limit_per_minute": strconv.Itoa(defaultRateLimit),
		"security.session_timeout":       strconv.Itoa(defaultSessionTimeout),
	}
	d.triggerSave()
}

// Close closes the database and saves to disk
func (d *Database) Close() error {
	d.mux.Lock()
	defer d.mux.Unlock()
	return d.save()
}

// save saves the database to disk
func (d *Database) save() error {
	data, err := json.MarshalIndent(d.data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal database: %w", err)
	}

	// Write to temporary file first
	tempPath := d.filePath + ".tmp"
	if err := os.WriteFile(tempPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write database: %w", err)
	}

	// Rename to actual file
	return os.Rename(tempPath, d.filePath)
}

// autoSaveLoop handles periodic auto-saving
func (d *Database) autoSaveLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			d.mux.RLock()
			d.save()
			d.mux.RUnlock()
		case <-d.autoSave:
			d.mux.RLock()
			d.save()
			d.mux.RUnlock()
		}
	}
}

// triggerSave triggers an immediate save
func (d *Database) triggerSave() {
	select {
	case d.autoSave <- struct{}{}:
	default:
	}
}

// ========== Config Management ==========

// GetConfig retrieves a configuration value by key
func (d *Database) GetConfig(key string) string {
	d.mux.RLock()
	defer d.mux.RUnlock()

	if val, ok := d.data.Config[key]; ok {
		return val
	}
	return ""
}

// SetConfig sets a configuration value by key
func (d *Database) SetConfig(key, value string) error {
	d.mux.Lock()
	defer d.mux.Unlock()

	d.data.Config[key] = value
	d.triggerSave()
	return nil
}

// GetAllConfig returns all configuration as a map
func (d *Database) GetAllConfig() map[string]string {
	d.mux.RLock()
	defer d.mux.RUnlock()

	// Return a copy to avoid race conditions
	result := make(map[string]string, len(d.data.Config))
	for k, v := range d.data.Config {
		result[k] = v
	}
	return result
}

// GetConfigInt returns a configuration value as integer
func (d *Database) GetConfigInt(key string) int {
	val := d.GetConfig(key)
	if val == "" {
		return 0
	}
	num, err := strconv.Atoi(val)
	if err != nil {
		return 0
	}
	return num
}

// SaveFileMetadata saves file metadata to the database
func (d *Database) SaveFileMetadata(meta *FileMetadata) error {
	d.mux.Lock()
	defer d.mux.Unlock()

	meta.ID = d.data.NextID
	d.data.NextID++

	d.data.Files[meta.ID] = meta
	d.triggerSave()

	return nil
}

// GetFileMetadata retrieves file metadata by path
func (d *Database) GetFileMetadata(filePath string) (*FileMetadata, error) {
	d.mux.RLock()
	defer d.mux.RUnlock()

	for _, meta := range d.data.Files {
		if meta.FilePath == filePath {
			return meta, nil
		}
	}
	return nil, nil
}

// GetFileMetadataByID retrieves file metadata by ID
func (d *Database) GetFileMetadataByID(id int64) (*FileMetadata, error) {
	d.mux.RLock()
	defer d.mux.RUnlock()

	meta, exists := d.data.Files[id]
	if !exists {
		return nil, nil
	}
	return meta, nil
}

// DeleteFileMetadata deletes file metadata by path
func (d *Database) DeleteFileMetadata(filePath string) error {
	d.mux.Lock()
	defer d.mux.Unlock()

	for id, meta := range d.data.Files {
		if meta.FilePath == filePath {
			delete(d.data.Files, id)
			d.triggerSave()
			return nil
		}
	}
	return nil
}

// GetExpiredFiles returns all files that have expired
func (d *Database) GetExpiredFiles() ([]*FileMetadata, error) {
	d.mux.RLock()
	defer d.mux.RUnlock()

	now := time.Now()
	var expired []*FileMetadata

	for _, meta := range d.data.Files {
		if meta.ExpiresAt.Before(now) {
			expired = append(expired, meta)
		}
	}

	return expired, nil
}

// ListFilesByDate returns all files for a specific date directory
func (d *Database) ListFilesByDate(date string) ([]*FileMetadata, error) {
	d.mux.RLock()
	defer d.mux.RUnlock()

	var files []*FileMetadata

	for _, meta := range d.data.Files {
		// Normalize path separators for comparison
		filePath := filepath.ToSlash(meta.FilePath)
		// Check if file starts with date + "/"
		if strings.HasPrefix(filePath, date+"/") {
			files = append(files, meta)
		}
	}

	return files, nil
}

// ListAllDates returns all unique date directories
func (d *Database) ListAllDates() ([]string, error) {
	d.mux.RLock()
	defer d.mux.RUnlock()

	dateMap := make(map[string]bool)

	for _, meta := range d.data.Files {
		// Extract date from path (YYYYMMDD/)
		// Normalize path separators first
		filePath := filepath.ToSlash(meta.FilePath)
		parts := strings.Split(filePath, "/")
		if len(parts) > 0 {
			dateMap[parts[0]] = true
		}
	}

	var dates []string
	for date := range dateMap {
		dates = append(dates, date)
	}

	return dates, nil
}

// GetStats returns database statistics
func (d *Database) GetStats() (totalFiles int, totalSize int64, err error) {
	d.mux.RLock()
	defer d.mux.RUnlock()

	totalFiles = len(d.data.Files)
	for _, meta := range d.data.Files {
		totalSize += meta.FileSize
	}

	return totalFiles, totalSize, nil
}

// GetGlobalDB returns the global database instance
func GetGlobalDB() *Database {
	return globalDB
}
