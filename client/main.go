package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var (
	version = "1.0.0"
)

// UploadResult represents the JSON output structure
type UploadResult struct {
	Status  string `json:"status"`  // "success" or "failed"
	Error   string `json:"error,omitempty"`   // Error message if failed
	Path    string `json:"path,omitempty"`    // File path if successful
	Message string `json:"message,omitempty"` // Additional information
	Time    int64  `json:"time"`    // Upload time in milliseconds
	Size    int64  `json:"size,omitempty"`    // File size in bytes
	Server  string `json:"server,omitempty"`  // Server address
}

func main() {
	// Preprocess args to handle common Windows command line issues
	args := preprocessArgs(os.Args)

	// Define command line flags
	var (
		flagServer  string
		flagAuth    string
		flagTTL     int
		flagVersion bool
		flagHelp    bool
	)

	flagSet := flag.NewFlagSet("http-cli", flag.ContinueOnError)
	flagSet.StringVar(&flagServer, "s", "http://localhost:8080", "Server address")
	flagSet.StringVar(&flagServer, "server", "http://localhost:8080", "Server address")
	flagSet.StringVar(&flagAuth, "a", "", "API authentication token (required)")
	flagSet.StringVar(&flagAuth, "auth", "", "API authentication token (required)")
	flagSet.IntVar(&flagTTL, "t", 1, "File TTL in hours (default: 1)")
	flagSet.IntVar(&flagTTL, "ttl", 1, "File TTL in hours (default: 1)")
	flagSet.BoolVar(&flagVersion, "v", false, "Show version information")
	flagSet.BoolVar(&flagVersion, "version", false, "Show version information")
	flagSet.BoolVar(&flagHelp, "h", false, "Show help information")
	flagSet.BoolVar(&flagHelp, "help", false, "Show help information")

	flagSet.Usage = printHelp

	// Parse flags
	if err := flagSet.Parse(args); err != nil {
		result := UploadResult{
			Status: "failed",
			Error:  err.Error(),
		}
		outputJSON(result)
		os.Exit(1)
		return
	}

	// Show version
	if flagVersion {
		result := UploadResult{
			Status:  "success",
			Message: fmt.Sprintf("HTTP Image Hosting Client v%s, Built for %s/%s", version, runtime.GOOS, runtime.GOARCH),
		}
		outputJSON(result)
		return
	}

	// Show help
	if flagHelp {
		printHelp()
		return
	}

	// Get file path (remaining args)
	filePathArgs := flagSet.Args()
	if len(filePathArgs) < 1 {
		result := UploadResult{
			Status: "failed",
			Error:  "file path is required",
		}
		outputJSON(result)
		os.Exit(1)
		return
	}

	filePath := filePathArgs[0]

	// Check API key
	if flagAuth == "" {
		result := UploadResult{
			Status: "failed",
			Error:  "API authentication token is required (-a flag)",
		}
		outputJSON(result)
		os.Exit(1)
		return
	}

	// Upload file
	result := uploadFile(filePath, flagServer, flagAuth, flagTTL)
	outputJSON(result)

	// Exit with error code if failed
	if result.Status == "failed" {
		os.Exit(1)
	}
}

// outputJSON prints the result as JSON to stdout
func outputJSON(result UploadResult) {
	data, err := json.Marshal(result)
	if err != nil {
		// Fallback to plain text if JSON marshaling fails
		fmt.Printf(`{"status":"failed","error":"failed to marshal output"}`)
		return
	}
	fmt.Println(string(data))
}

// preprocessArgs preprocesses arguments to handle Windows command line issues
func preprocessArgs(originalArgs []string) []string {
	if len(originalArgs) <= 1 {
		return originalArgs[1:]
	}

	// Skip the program name (first argument)
	args := originalArgs[1:]

	// Reorder args: move all flags to the front, file path to the end
	var flags []string
	var filePath string
	var otherArgs []string

	// First, identify flags and file path
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			// It's a flag
			flags = append(flags, arg)
			// Check if next arg is a value (not a flag)
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				flags = append(flags, args[i])
			}
		} else if filePath == "" && !strings.Contains(arg, "/") && !strings.Contains(arg, "\\") {
			// Could be a file path (no slashes)
			// Check if file exists
			if _, err := os.Stat(arg); err == nil {
				filePath = arg
			} else {
				otherArgs = append(otherArgs, arg)
			}
		} else if strings.Contains(arg, "/") || strings.Contains(arg, "\\") {
			// It's likely a file path
			if filePath == "" {
				filePath = arg
			} else {
				otherArgs = append(otherArgs, arg)
			}
		} else {
			otherArgs = append(otherArgs, arg)
		}
	}

	// If we found a file path among the args, move it to the end
	if filePath != "" {
		result := append(flags, otherArgs...)
		result = append(result, filePath)
		return result
	}

	// No file path found, return original
	return args
}

// uploadFile uploads a file to the server
func uploadFile(filePath, serverURL, authToken string, ttl int) UploadResult {
	startTime := time.Now()
	result := UploadResult{
		Server: serverURL,
		Status: "failed",
	}

	// Get file info
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		result.Error = fmt.Sprintf("failed to access file: %v", err)
		result.Time = time.Since(startTime).Milliseconds()
		return result
	}

	if fileInfo.IsDir() {
		result.Error = "path is a directory, not a file"
		result.Time = time.Since(startTime).Milliseconds()
		return result
	}

	result.Size = fileInfo.Size()

	// Get absolute path
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		result.Error = fmt.Sprintf("failed to get absolute path: %v", err)
		result.Time = time.Since(startTime).Milliseconds()
		return result
	}

	// Get filename
	filename := filepath.Base(absPath)

	// Open file
	file, err := os.Open(absPath)
	if err != nil {
		result.Error = fmt.Sprintf("failed to open file: %v", err)
		result.Time = time.Since(startTime).Milliseconds()
		return result
	}
	defer file.Close()

	// Create multipart form body
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	// Create form file
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		result.Error = fmt.Sprintf("failed to create form file: %v", err)
		result.Time = time.Since(startTime).Milliseconds()
		return result
	}

	// Copy file content
	_, err = io.Copy(part, file)
	if err != nil {
		result.Error = fmt.Sprintf("failed to copy file content: %v", err)
		result.Time = time.Since(startTime).Milliseconds()
		return result
	}

	// Add TTL field
	writer.WriteField("ttl", fmt.Sprintf("%d", ttl))
	writer.WriteField("filename", filename)

	// Close multipart writer
	if err := writer.Close(); err != nil {
		result.Error = fmt.Sprintf("failed to close multipart writer: %v", err)
		result.Time = time.Since(startTime).Milliseconds()
		return result
	}

	// Create request
	serverURL = strings.TrimRight(serverURL, "/")
	url := serverURL + "/upload"

	req, err := http.NewRequest("POST", url, &body)
	if err != nil {
		result.Error = fmt.Sprintf("failed to create request: %v", err)
		result.Time = time.Since(startTime).Milliseconds()
		return result
	}

	// Set headers
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-API-Key", authToken)

	// Execute request
	client := &http.Client{
		Timeout: 5 * time.Minute,
	}

	resp, err := client.Do(req)
	if err != nil {
		result.Error = fmt.Sprintf("upload failed: %v", err)
		result.Time = time.Since(startTime).Milliseconds()
		return result
	}
	defer resp.Body.Close()

	// Read response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		result.Error = fmt.Sprintf("failed to read response: %v", err)
		result.Time = time.Since(startTime).Milliseconds()
		return result
	}

	// Parse response
	var serverResult struct {
		Success   bool   `json:"success"`
		Message   string `json:"message"`
		FilePath  string `json:"file_path"`
		ExpiresAt string `json:"expires_at"`
	}

	if err := json.Unmarshal(respBody, &serverResult); err != nil {
		result.Error = fmt.Sprintf("failed to parse response: %v", err)
		result.Time = time.Since(startTime).Milliseconds()
		return result
	}

	// Check response
	if resp.StatusCode != http.StatusOK {
		result.Error = fmt.Sprintf("server error (%d): %s", resp.StatusCode, serverResult.Message)
		result.Time = time.Since(startTime).Milliseconds()
		return result
	}

	if !serverResult.Success {
		result.Error = fmt.Sprintf("upload failed: %s", serverResult.Message)
		result.Time = time.Since(startTime).Milliseconds()
		return result
	}

	// Success
	result.Status = "success"
	result.Path = serverResult.FilePath
	result.Message = serverResult.Message
	result.Time = time.Since(startTime).Milliseconds()
	if serverResult.ExpiresAt != "" {
		result.Message = fmt.Sprintf("%s (expires at: %s)", result.Message, serverResult.ExpiresAt)
	}

	return result
}

func printHelp() {
	fmt.Printf("HTTP Image Hosting Client v%s\n\n", version)
	fmt.Println("Usage:")
	fmt.Println("  http-cli [options] <file_path>")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  -a, --auth <token>    API authentication token (required)")
	fmt.Println("  -s, --server <url>    Server address (default: http://localhost:8080)")
	fmt.Println("  -t, --ttl <hours>     File TTL in hours (default: 1, max: 8760)")
	fmt.Println("  -v, --version         Show version information")
	fmt.Println("  -h, --help            Show this help message")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  http-cli -a my-token photo.jpg")
	fmt.Println("  http-cli -a abc123 -t 24 C:/Users/Zoo/image.png")
	fmt.Println("  http-cli -a my-token -s http://192.168.1.100:8080 -t 48 photo.jpg")
}
