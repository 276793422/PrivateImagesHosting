package httpd

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"httpserver/server/config"
	"httpserver/server/db"
	"httpserver/server/naming"
)

// Server represents the HTTP server
type Server struct {
	cfg         *config.Config
	db          *db.Database
	server      *http.Server
	sessions    map[string]time.Time // session token -> expiry
	sessionMux  sync.RWMutex
}

// NewServer creates a new HTTP server
func NewServer(cfg *config.Config, database *db.Database) *Server {
	mux := http.NewServeMux()

	s := &Server{
		cfg:      cfg,
		db:       database,
		sessions: make(map[string]time.Time),
	}

	// Register routes
	mux.HandleFunc("/upload", s.handleUpload)
	mux.HandleFunc("/files/", s.handleFiles)
	mux.HandleFunc("/api/files", s.handleAPIFiles)
	mux.HandleFunc("/api/login", s.handleLogin)
	mux.HandleFunc("/api/admin/", s.handleAdminAPI)
	mux.HandleFunc("/list.html", s.handleListPage)
	mux.HandleFunc("/manager.html", s.handleManagerPage)
	mux.HandleFunc("/health", s.handleHealth)
	// Register catch-all route for root and direct file access
	mux.HandleFunc("/", s.handleCatchAll)

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	s.server = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	// Start session cleanup goroutine
	go s.cleanupSessions()

	return s
}

// Start starts the HTTP server
func (s *Server) Start() error {
	log.Printf("Starting HTTP server on %s", s.server.Addr)
	return s.server.ListenAndServe()
}

// handleUpload handles file upload requests
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check API Key
	apiKey := r.Header.Get("X-API-Key")
	if apiKey != s.cfg.Auth.APIKey {
		s.writeJSONError(w, http.StatusUnauthorized, "Invalid or missing API key")
		return
	}

	// Parse multipart form (max 100MB)
	if err := r.ParseMultipartForm(s.cfg.Storage.MaxFileSize); err != nil {
		s.writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Failed to parse form: %v", err))
		return
	}

	// Get file from form
	file, header, err := r.FormFile("file")
	if err != nil {
		s.writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Failed to get file: %v", err))
		return
	}
	defer file.Close()

	// Get TTL
	ttlStr := r.FormValue("ttl")
	ttl := s.cfg.Storage.DefaultTTL
	if ttlStr != "" {
		ttl, err = strconv.Atoi(ttlStr)
		if err != nil {
			s.writeJSONError(w, http.StatusBadRequest, "Invalid TTL value")
			return
		}
	}

	// Validate TTL
	if ttl < 1 || ttl > s.cfg.Storage.MaxTTL {
		s.writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("TTL must be between 1 and %d hours", s.cfg.Storage.MaxTTL))
		return
	}

	// Generate file path
	relativePath, err := naming.GenerateFilePath(header.Filename)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to generate file path: %v", err))
		return
	}

	// Create date directory
	dateDir := naming.ParseDateFromPath(relativePath)
	fullDirPath := filepath.Join(s.cfg.Storage.ImagesDir, dateDir)
	if err := os.MkdirAll(fullDirPath, 0755); err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to create directory: %v", err))
		return
	}

	// Save file
	fullPath := naming.GetStoragePath(s.cfg.Storage.ImagesDir, relativePath)
	dst, err := os.Create(fullPath)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to create file: %v", err))
		return
	}
	defer dst.Close()

	size, err := io.Copy(dst, file)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to save file: %v", err))
		return
	}

	// Calculate expiry time
	uploadedAt := time.Now()
	expiresAt := uploadedAt.Add(time.Duration(ttl) * time.Hour)

	// Save metadata to database
	metadata := &db.FileMetadata{
		FileName:     filepath.Base(relativePath),
		OriginalName: header.Filename,
		FilePath:     relativePath,
		FileSize:     size,
		UploadedAt:   uploadedAt,
		ExpiresAt:    expiresAt,
		TTL:          ttl,
		RemoteIP:     getRemoteIP(r),
	}

	if err := s.db.SaveFileMetadata(metadata); err != nil {
		log.Printf("Warning: failed to save metadata: %v", err)
	}

	// Return success response
	response := map[string]interface{}{
		"success":     true,
		"message":     "File uploaded successfully",
		"file_path":   relativePath,
		"download_url": fmt.Sprintf("/files/%s", relativePath),
		"expires_at":  expiresAt.Format(time.RFC3339),
	}

	s.writeJSON(w, http.StatusOK, response)
	log.Printf("File uploaded: %s (original: %s, size: %d bytes, TTL: %dh)", relativePath, header.Filename, size, ttl)
}

// handleFiles handles file download requests
func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract file path from URL
	filePath := strings.TrimPrefix(r.URL.Path, "/files/")
	if filePath == "" {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	// Build full file path
	fullPath := naming.GetStoragePath(s.cfg.Storage.ImagesDir, filePath)

	// Check if file exists
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	// Set content type
	ext := filepath.Ext(filePath)
	mimeType := mime.TypeByExtension(ext)
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", mimeType)

	// Serve file
	http.ServeFile(w, r, fullPath)
	log.Printf("File downloaded: %s from %s", filePath, getRemoteIP(r))
}

// handleAPIFiles handles the file list API
func (s *Server) handleAPIFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check session
	if !s.checkSession(w, r) {
		return
	}

	// Get date parameter
	date := r.URL.Query().Get("path")

	var files []*db.FileMetadata
	var dates []string
	var err error

	if date != "" {
		// List files in specific date directory
		files, err = s.db.ListFilesByDate(date)
		if err != nil {
			s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to list files: %v", err))
			return
		}
	} else {
		// List all date directories
		dates, err = s.db.ListAllDates()
		if err != nil {
			s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to list dates: %v", err))
			return
		}
	}

	response := map[string]interface{}{
		"success":      true,
		"current_path": date,
		"files":        files,
		"directories":  dates,
	}

	s.writeJSON(w, http.StatusOK, response)
}

// handleLogin handles login requests
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeJSONError(w, http.StatusBadRequest, "Invalid request")
		return
	}

	if req.Password != s.cfg.Auth.ListPassword {
		s.writeJSONError(w, http.StatusUnauthorized, "Invalid password")
		return
	}

	// Generate session token
	token := generateToken()

	// Store session with expiry
	s.sessionMux.Lock()
	s.sessions[token] = time.Now().Add(time.Duration(s.cfg.Security.SessionTimeout) * time.Second)
	s.sessionMux.Unlock()

	// Set cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "session_token",
		Value:    token,
		MaxAge:   s.cfg.Security.SessionTimeout,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	s.writeJSON(w, http.StatusOK, map[string]bool{"success": true})
	log.Printf("User logged in from %s", getRemoteIP(r))
}

// handleAdminAPI handles admin API requests
func (s *Server) handleAdminAPI(w http.ResponseWriter, r *http.Request) {
	// Basic auth for admin
	username, password, ok := r.BasicAuth()
	if !ok || username != s.cfg.Auth.AdminUsername || password != s.cfg.Auth.AdminPassword {
		w.Header().Set("WWW-Authenticate", `Basic realm="Admin"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Handle different admin endpoints
	switch {
	case strings.HasSuffix(r.URL.Path, "/config"):
		s.handleAdminConfig(w, r)
	case strings.HasSuffix(r.URL.Path, "/stats"):
		s.handleAdminStats(w, r)
	case strings.HasSuffix(r.URL.Path, "/logs"):
		s.handleAdminLogs(w, r)
	default:
		http.Error(w, "Not found", http.StatusNotFound)
	}
}

// handleAdminConfig handles config management
func (s *Server) handleAdminConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		s.writeJSON(w, http.StatusOK, s.cfg)
	} else if r.Method == http.MethodPut {
		// Update config (implementation needed)
		s.writeJSON(w, http.StatusOK, map[string]bool{"success": true})
	} else {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleAdminStats handles stats requests
func (s *Server) handleAdminStats(w http.ResponseWriter, r *http.Request) {
	totalFiles, totalSize, err := s.db.GetStats()
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to get stats: %v", err))
		return
	}

	response := map[string]interface{}{
		"total_files": totalFiles,
		"total_size":  totalSize,
	}

	s.writeJSON(w, http.StatusOK, response)
}

// handleAdminLogs handles log requests
func (s *Server) handleAdminLogs(w http.ResponseWriter, r *http.Request) {
	// Return recent logs (implementation needed)
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"logs": []string{},
	})
}

// handleListPage handles the file list page
func (s *Server) handleListPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(listPageHTML))
}

// handleManagerPage handles the admin manager page
func (s *Server) handleManagerPage(w http.ResponseWriter, r *http.Request) {
	// Check basic auth
	username, password, ok := r.BasicAuth()
	if !ok || username != s.cfg.Auth.AdminUsername || password != s.cfg.Auth.AdminPassword {
		w.Header().Set("WWW-Authenticate", `Basic realm="Admin"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(managerPageHTML))
}

// handleHealth handles health check requests
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	totalFiles, totalSize, _ := s.db.GetStats()

	response := map[string]interface{}{
		"status": "ok",
		"storage_info": map[string]interface{}{
			"total_files": totalFiles,
			"total_size":  formatBytes(totalSize),
		},
	}

	s.writeJSON(w, http.StatusOK, response)
}

// handleCatchAll handles root path and direct file access
func (s *Server) handleCatchAll(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" {
		// Root path - serve home page or redirect to list page
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(rootPageHTML))
		return
	}

	// Check if request is for a file (pattern: YYYYMMDD/filename.ext)
	requestPath := strings.TrimPrefix(r.URL.Path, "/")
	parts := strings.Split(requestPath, "/")

	// Check if pattern matches: date directory + file with extension
	if len(parts) >= 2 && len(parts[0]) == 8 && isAllDigits(parts[0]) && filepath.Ext(parts[1]) != "" {
		// This looks like a direct file access request
		// Delegate to handleFiles logic
		s.handleFiles(w, r)
		return
	}

	// Not found
	http.NotFound(w, r)
}

func isAllDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// checkSession checks if the user has a valid session
func (s *Server) checkSession(w http.ResponseWriter, r *http.Request) bool {
	cookie, err := r.Cookie("session_token")
	if err != nil {
		s.writeJSONError(w, http.StatusUnauthorized, "Not authenticated")
		return false
	}

	s.sessionMux.RLock()
	expiresAt, exists := s.sessions[cookie.Value]
	s.sessionMux.RUnlock()

	if !exists || time.Now().After(expiresAt) {
		s.writeJSONError(w, http.StatusUnauthorized, "Session expired")
		return false
	}

	return true
}

// cleanupSessions removes expired sessions
func (s *Server) cleanupSessions() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		s.sessionMux.Lock()
		now := time.Now()
		for token, expiresAt := range s.sessions {
			if now.After(expiresAt) {
				delete(s.sessions, token)
			}
		}
		s.sessionMux.Unlock()
	}
}

// generateToken generates a random session token
func generateToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	hash := sha256.Sum256(b)
	return base64.URLEncoding.EncodeToString(hash[:])
}

// writeJSON writes a JSON response
func (s *Server) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// writeJSONError writes a JSON error response
func (s *Server) writeJSONError(w http.ResponseWriter, status int, message string) {
	s.writeJSON(w, status, map[string]interface{}{
		"success": false,
		"message": message,
	})
}

// getRemoteIP gets the remote IP address
func getRemoteIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		return strings.Split(forwarded, ",")[0]
	}
	return r.RemoteAddr
}

// formatBytes formats bytes to human readable string
func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// HTML pages (embedded)
const rootPageHTML = `<!DOCTYPE html>
<html>
<head><title>HTTP Image Hosting</title></head>
<body><h1>HTTP Image Hosting Server</h1><p><a href="/list.html">File List</a></p></body>
</html>`

const listPageHTML = `<!DOCTYPE html>
<html>
<head>
    <title>File List - HTTP Image Hosting</title>
    <meta charset="UTF-8">
    <style>
        body { font-family: Arial, sans-serif; margin: 20px; }
        .login-overlay { position: fixed; top: 0; left: 0; width: 100%; height: 100%; background: rgba(0,0,0,0.5); display: flex; justify-content: center; align-items: center; }
        .login-box { background: white; padding: 30px; border-radius: 8px; box-shadow: 0 2px 10px rgba(0,0,0,0.1); }
        .login-box input { padding: 10px; margin: 10px 0; width: 200px; }
        .login-box button { padding: 10px 20px; background: #007bff; color: white; border: none; border-radius: 4px; cursor: pointer; }
        .file-list { margin-top: 20px; }
        .file-item { padding: 10px; border-bottom: 1px solid #eee; display: flex; justify-content: space-between; }
        .file-item a { color: #007bff; text-decoration: none; }
        .file-item a:hover { text-decoration: underline; }
        .dir-item { padding: 10px; border-bottom: 1px solid #eee; }
        .dir-item a { color: #333; text-decoration: none; font-weight: bold; }
        .hidden { display: none; }
    </style>
</head>
<body>
    <h1>File List</h1>
    <button onclick="logout()">Logout</button>
    <div id="login-overlay" class="login-overlay">
        <div class="login-box">
            <h2>Login Required</h2>
            <input type="password" id="password" placeholder="Enter password" onkeypress="if(event.key==='Enter') login()">
            <br><button onclick="login()">Login</button>
        </div>
    </div>
    <div id="content" class="hidden">
        <p>Current: <span id="current-path">/</span> <a href="#" onclick="loadFiles('')">[Root]</a></p>
        <div id="file-list"></div>
    </div>

    <script>
        async function login() {
            const password = document.getElementById('password').value;
            const res = await fetch('/api/login', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ password })
            });
            if (res.ok) {
                document.getElementById('login-overlay').classList.add('hidden');
                document.getElementById('content').classList.remove('hidden');
                loadFiles('');
            } else {
                alert('Invalid password');
            }
        }

        async function loadFiles(path) {
            const res = await fetch('/api/files?path=' + encodeURIComponent(path));
            const data = await res.json();
            document.getElementById('current-path').textContent = path || '/';
            const list = document.getElementById('file-list');
            list.innerHTML = '';

            data.directories.forEach(dir => {
                const div = document.createElement('div');
                div.className = 'dir-item';
                div.innerHTML = '<a href="#" onclick="loadFiles(\'' + dir + '\')">📁 ' + dir + '</a>';
                list.appendChild(div);
            });

            data.files.forEach(file => {
                const div = document.createElement('div');
                div.className = 'file-item';
                const size = formatSize(file.file_size);
                const expires = new Date(file.expires_at).toLocaleString();
                div.innerHTML = '<a href="/files/' + file.file_path + '" download>' + file.file_name + '</a> <span>' + size + ' | Expires: ' + expires + '</span>';
                list.appendChild(div);
            });
        }

        function logout() {
            document.cookie = 'session_token=; expires=Thu, 01 Jan 1970 00:00:00 UTC; path=/;';
            location.reload();
        }

        function formatSize(bytes) {
            if (bytes < 1024) return bytes + ' B';
            if (bytes < 1024*1024) return (bytes/1024).toFixed(1) + ' KB';
            return (bytes/(1024*1024)).toFixed(1) + ' MB';
        }

        // Check session on load
        fetch('/api/files').then(res => {
            if (res.ok) {
                document.getElementById('login-overlay').classList.add('hidden');
                document.getElementById('content').classList.remove('hidden');
                loadFiles('');
            }
        });
    </script>
</body>
</html>`

const managerPageHTML = `<!DOCTYPE html>
<html>
<head>
    <title>Admin Manager - HTTP Image Hosting</title>
    <meta charset="UTF-8">
    <style>
        body { font-family: Arial, sans-serif; margin: 20px; }
        .section { margin: 20px 0; padding: 15px; border: 1px solid #ddd; border-radius: 5px; }
        h2 { color: #333; }
        button { padding: 8px 15px; background: #007bff; color: white; border: none; border-radius: 4px; cursor: pointer; margin-right: 10px; }
        button:hover { background: #0056b3; }
        .stat { display: inline-block; margin: 10px 20px 10px 0; }
        .stat-label { font-weight: bold; }
    </style>
</head>
<body>
    <h1>HTTP Image Hosting - Admin Manager</h1>

    <div class="section">
        <h2>Statistics</h2>
        <div class="stat"><span class="stat-label">Total Files:</span> <span id="total-files">-</span></div>
        <div class="stat"><span class="stat-label">Total Size:</span> <span id="total-size">-</span></div>
        <button onclick="loadStats()">Refresh</button>
    </div>

    <div class="section">
        <h2>Configuration</h2>
        <button onclick="loadConfig()">Load Config</button>
        <button onclick="showConfigForm()">Edit Config</button>
        <pre id="config-display"></pre>
    </div>

    <div class="section">
        <h2>Actions</h2>
        <button onclick="cleanupExpired()">Cleanup Expired Files</button>
    </div>

    <script>
        async function loadStats() {
            const res = await fetch('/api/admin/stats');
            const data = await res.json();
            document.getElementById('total-files').textContent = data.total_files;
            document.getElementById('total-size').textContent = formatSize(data.total_size);
        }

        async function loadConfig() {
            const res = await fetch('/api/admin/config');
            const data = await res.json();
            document.getElementById('config-display').textContent = JSON.stringify(data, null, 2);
        }

        function showConfigForm() {
            alert('Config editing UI to be implemented');
        }

        function formatSize(bytes) {
            if (bytes < 1024) return bytes + ' B';
            if (bytes < 1024*1024) return (bytes/1024).toFixed(1) + ' KB';
            return (bytes/(1024*1024)).toFixed(1) + ' MB';
        }

        loadStats();
        loadConfig();
    </script>
</body>
</html>`
