package files

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/effectus/effectus-go/adapters"
)

// FileWatcherSource monitors file system changes
type FileWatcherSource struct {
	sourceID    string
	sourceType  string
	paths       []string
	patterns    []string
	events      []string
	schemaName  string
	maxFileSize int64
	recursive   bool

	watcher *fsnotify.Watcher
	ctx     context.Context
	cancel  context.CancelFunc
	schema  *adapters.Schema
}

// WatcherConfig holds configuration for file system watcher
type WatcherConfig struct {
	Paths       []string `json:"paths" yaml:"paths"`
	Patterns    []string `json:"patterns" yaml:"patterns"`
	Events      []string `json:"events" yaml:"events"`
	SchemaName  string   `json:"schema_name" yaml:"schema_name"`
	MaxFileSize int64    `json:"max_file_size" yaml:"max_file_size"`
	Recursive   bool     `json:"recursive" yaml:"recursive"`
}

// FileEvent represents a file system event
type FileEvent struct {
	Path      string    `json:"path"`
	Name      string    `json:"name"`
	Operation string    `json:"operation"`
	Size      int64     `json:"size"`
	ModTime   time.Time `json:"mod_time"`
	IsDir     bool      `json:"is_dir"`
	Content   string    `json:"content,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// NewFileWatcherSource creates a new file system watcher source
func NewFileWatcherSource(sourceID string, config WatcherConfig) (*FileWatcherSource, error) {
	if len(config.Paths) == 0 {
		return nil, fmt.Errorf("at least one path is required")
	}

	// Set defaults
	if len(config.Events) == 0 {
		config.Events = []string{"CREATE", "WRITE", "REMOVE", "RENAME"}
	}
	if config.SchemaName == "" {
		config.SchemaName = "file_system_event"
	}
	if config.MaxFileSize == 0 {
		config.MaxFileSize = 10 * 1024 * 1024 // 10MB default
	}

	// Validate paths
	for _, path := range config.Paths {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return nil, fmt.Errorf("path does not exist: %s", path)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())

	source := &FileWatcherSource{
		sourceID:    sourceID,
		sourceType:  "file_watcher",
		paths:       config.Paths,
		patterns:    config.Patterns,
		events:      config.Events,
		schemaName:  config.SchemaName,
		maxFileSize: config.MaxFileSize,
		recursive:   config.Recursive,
		ctx:         ctx,
		cancel:      cancel,
		schema: &adapters.Schema{
			Name:    config.SchemaName,
			Version: "v1.0.0",
			Fields: map[string]interface{}{
				"path":      "string",
				"name":      "string",
				"operation": "string",
				"size":      "integer",
				"timestamp": "datetime",
				"content":   "string",
			},
		},
	}

	return source, nil
}

func (f *FileWatcherSource) Subscribe(ctx context.Context, factTypes []string) (<-chan *adapters.TypedFact, error) {
	factChan := make(chan *adapters.TypedFact, 100)

	// Start watching in background
	go func() {
		defer close(factChan)

		if err := f.watchFiles(factChan); err != nil {
			log.Printf("File watching failed: %v", err)
		}
	}()

	return factChan, nil
}

func (f *FileWatcherSource) Start(ctx context.Context) error {
	// Create file system watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create file watcher: %w", err)
	}
	f.watcher = watcher

	// Add paths to watcher
	for _, path := range f.paths {
		if err := f.addPath(path); err != nil {
			log.Printf("Warning: failed to add path %s: %v", path, err)
		}
	}

	log.Printf("File watcher source started, watching paths: %v", f.paths)
	return nil
}

func (f *FileWatcherSource) Stop(ctx context.Context) error {
	f.cancel()

	if f.watcher != nil {
		if err := f.watcher.Close(); err != nil {
			return fmt.Errorf("failed to close file watcher: %w", err)
		}
	}

	log.Printf("File watcher source stopped")
	return nil
}

func (f *FileWatcherSource) GetSourceSchema() *adapters.Schema {
	return f.schema
}

func (f *FileWatcherSource) HealthCheck() error {
	if f.watcher == nil {
		return fmt.Errorf("file watcher not initialized")
	}

	// Check if paths are still accessible
	for _, path := range f.paths {
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("path %s is not accessible: %w", path, err)
		}
	}

	return nil
}

func (f *FileWatcherSource) GetMetadata() adapters.SourceMetadata {
	return adapters.SourceMetadata{
		SourceID:      f.sourceID,
		SourceType:    f.sourceType,
		Version:       "1.0.0",
		Capabilities:  []string{"streaming", "realtime", "filesystem"},
		SchemaFormats: []string{"json"},
		Config: map[string]string{
			"paths":     strings.Join(f.paths, ","),
			"patterns":  strings.Join(f.patterns, ","),
			"events":    strings.Join(f.events, ","),
			"recursive": fmt.Sprintf("%t", f.recursive),
		},
		Tags: []string{"filesystem", "watcher"},
	}
}

func (f *FileWatcherSource) addPath(path string) error {
	if f.recursive {
		return filepath.Walk(path, func(walkPath string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return f.watcher.Add(walkPath)
			}
			return nil
		})
	} else {
		return f.watcher.Add(path)
	}
}

func (f *FileWatcherSource) watchFiles(factChan chan<- *adapters.TypedFact) error {
	for {
		select {
		case <-f.ctx.Done():
			return nil
		case event, ok := <-f.watcher.Events:
			if !ok {
				return fmt.Errorf("watcher events channel closed")
			}

			if f.shouldProcessEvent(event) {
				if fact, err := f.transformEvent(event); err == nil {
					select {
					case factChan <- fact:
						// Event sent
					case <-f.ctx.Done():
						return nil
					default:
						log.Printf("Fact channel full, dropping file event")
					}
				} else {
					log.Printf("Failed to transform file event: %v", err)
				}
			}

		case err, ok := <-f.watcher.Errors:
			if !ok {
				return fmt.Errorf("watcher errors channel closed")
			}
			log.Printf("File watcher error: %v", err)
		}
	}
}

func (f *FileWatcherSource) shouldProcessEvent(event fsnotify.Event) bool {
	// Check if operation is enabled
	operation := f.getOperationName(event.Op)
	enabled := false
	for _, enabledOp := range f.events {
		if strings.EqualFold(enabledOp, operation) {
			enabled = true
			break
		}
	}
	if !enabled {
		return false
	}

	// Check patterns if specified
	if len(f.patterns) > 0 {
		filename := filepath.Base(event.Name)
		matched := false
		for _, pattern := range f.patterns {
			if matched, _ := filepath.Match(pattern, filename); matched {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	return true
}

func (f *FileWatcherSource) getOperationName(op fsnotify.Op) string {
	switch {
	case op&fsnotify.Create == fsnotify.Create:
		return "CREATE"
	case op&fsnotify.Write == fsnotify.Write:
		return "WRITE"
	case op&fsnotify.Remove == fsnotify.Remove:
		return "REMOVE"
	case op&fsnotify.Rename == fsnotify.Rename:
		return "RENAME"
	case op&fsnotify.Chmod == fsnotify.Chmod:
		return "CHMOD"
	default:
		return "UNKNOWN"
	}
}

func (f *FileWatcherSource) transformEvent(event fsnotify.Event) (*adapters.TypedFact, error) {
	// Get file info
	var fileInfo os.FileInfo
	var err error
	var content string

	// Get file info if file still exists
	if event.Op&fsnotify.Remove != fsnotify.Remove {
		fileInfo, err = os.Stat(event.Name)
		if err != nil {
			// File might have been removed between event and stat
			log.Printf("Warning: cannot stat file %s: %v", event.Name, err)
		}
	}

	// Read file content for small files on CREATE/WRITE events
	if fileInfo != nil && !fileInfo.IsDir() {
		if (event.Op&fsnotify.Create == fsnotify.Create || event.Op&fsnotify.Write == fsnotify.Write) &&
			fileInfo.Size() <= f.maxFileSize {
			if contentBytes, err := ioutil.ReadFile(event.Name); err == nil {
				content = string(contentBytes)
			}
		}
	}

	// Create file event
	fileEvent := &FileEvent{
		Path:      event.Name,
		Name:      filepath.Base(event.Name),
		Operation: f.getOperationName(event.Op),
		Timestamp: time.Now(),
		Content:   content,
	}

	if fileInfo != nil {
		fileEvent.Size = fileInfo.Size()
		fileEvent.ModTime = fileInfo.ModTime()
		fileEvent.IsDir = fileInfo.IsDir()
	}

	// Serialize event data
	data, err := json.Marshal(fileEvent)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal file event: %w", err)
	}

	return &adapters.TypedFact{
		SchemaName:    f.schemaName,
		SchemaVersion: "v1.0.0",
		Data:          nil, // Would contain proto message in real implementation
		RawData:       data,
		Timestamp:     fileEvent.Timestamp,
		SourceID:      f.sourceID,
		TraceID:       "",
		Metadata: map[string]string{
			"file.path":      event.Name,
			"file.name":      filepath.Base(event.Name),
			"file.operation": fileEvent.Operation,
			"file.size":      fmt.Sprintf("%d", fileEvent.Size),
			"file.is_dir":    fmt.Sprintf("%t", fileEvent.IsDir),
			"source_type":    "file_watcher",
		},
	}, nil
}

// FileWatcherFactory creates file watcher sources
type FileWatcherFactory struct{}

func (f *FileWatcherFactory) Create(config adapters.SourceConfig) (adapters.FactSource, error) {
	watcherConfig := WatcherConfig{}

	// Extract configuration
	if paths, ok := config.Config["paths"].([]interface{}); ok {
		watcherConfig.Paths = make([]string, len(paths))
		for i, path := range paths {
			if pathStr, ok := path.(string); ok {
				watcherConfig.Paths[i] = pathStr
			}
		}
	}
	if patterns, ok := config.Config["patterns"].([]interface{}); ok {
		watcherConfig.Patterns = make([]string, len(patterns))
		for i, pattern := range patterns {
			if patternStr, ok := pattern.(string); ok {
				watcherConfig.Patterns[i] = patternStr
			}
		}
	}
	if events, ok := config.Config["events"].([]interface{}); ok {
		watcherConfig.Events = make([]string, len(events))
		for i, event := range events {
			if eventStr, ok := event.(string); ok {
				watcherConfig.Events[i] = eventStr
			}
		}
	}
	if schemaName, ok := config.Config["schema_name"].(string); ok {
		watcherConfig.SchemaName = schemaName
	}
	if maxFileSize, ok := config.Config["max_file_size"].(float64); ok {
		watcherConfig.MaxFileSize = int64(maxFileSize)
	}
	if recursive, ok := config.Config["recursive"].(bool); ok {
		watcherConfig.Recursive = recursive
	}

	return NewFileWatcherSource(config.SourceID, watcherConfig)
}

func (f *FileWatcherFactory) ValidateConfig(config adapters.SourceConfig) error {
	if paths, ok := config.Config["paths"].([]interface{}); !ok || len(paths) == 0 {
		return fmt.Errorf("at least one path is required for file_watcher source")
	}
	return nil
}

func (f *FileWatcherFactory) GetConfigSchema() adapters.ConfigSchema {
	return adapters.ConfigSchema{
		Properties: map[string]adapters.ConfigProperty{
			"paths": {
				Type:        "array",
				Description: "Paths to watch for file changes",
				Examples:    []string{`["/var/log", "/etc/config"]`, `["./data", "./uploads"]`},
			},
			"patterns": {
				Type:        "array",
				Description: "File patterns to match (optional)",
				Examples:    []string{`["*.log", "*.json"]`, `["config.*"]`},
			},
			"events": {
				Type:        "array",
				Description: "File system events to capture",
				Default:     `["CREATE", "WRITE", "REMOVE", "RENAME"]`,
				Examples:    []string{`["CREATE", "WRITE"]`, `["REMOVE"]`},
			},
			"schema_name": {
				Type:        "string",
				Description: "Schema name for generated facts",
				Default:     "file_system_event",
			},
			"max_file_size": {
				Type:        "int",
				Description: "Maximum file size to read content (bytes)",
				Default:     10485760, // 10MB
			},
			"recursive": {
				Type:        "bool",
				Description: "Watch directories recursively",
				Default:     false,
			},
		},
		Required: []string{"paths"},
	}
}

func init() {
	adapters.RegisterSourceType("file_watcher", &FileWatcherFactory{})
}
