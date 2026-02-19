package sourceingester

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semspec/source/parser"
	"github.com/fsnotify/fsnotify"
)

const (
	// eventChannelBuffer is the size of the watch event channel.
	eventChannelBuffer = 500
)

// WatchConfig configures document file watching.
type WatchConfig struct {
	// Enabled controls whether file watching is active.
	Enabled bool `json:"enabled" schema:"type:bool,description:Enable file watching for automatic document indexing,category:advanced,default:false"`

	// DebounceDelay is how long to wait for more changes before processing.
	DebounceDelay string `json:"debounce_delay" schema:"type:string,description:Debounce delay before processing file changes,category:advanced,default:500ms"`

	// FileExtensions lists file extensions to watch (e.g., [".md", ".txt", ".pdf"]).
	FileExtensions []string `json:"file_extensions" schema:"type:array,description:File extensions to watch for document changes,category:advanced,default:[.md,.txt]"`

	// ExcludeDirs lists directory names to skip (e.g., [".git", "node_modules"]).
	ExcludeDirs []string `json:"exclude_dirs" schema:"type:array,description:Directory names to exclude from watching,category:advanced,default:[.git,node_modules,vendor]"`
}

// DefaultWatchConfig returns default watch configuration.
func DefaultWatchConfig() WatchConfig {
	return WatchConfig{
		Enabled:        false,
		DebounceDelay:  "500ms",
		FileExtensions: []string{".md", ".txt"},
		ExcludeDirs:    []string{".git", "node_modules", "vendor"},
	}
}

// GetDebounceDelay returns the debounce delay as a duration.
func (c *WatchConfig) GetDebounceDelay() time.Duration {
	if c.DebounceDelay == "" {
		return 500 * time.Millisecond
	}
	d, err := time.ParseDuration(c.DebounceDelay)
	if err != nil {
		return 500 * time.Millisecond
	}
	return d
}

// WatchEvent represents a document file change event.
type WatchEvent struct {
	// Path is the file path relative to sources directory.
	Path string

	// Operation is the type of change.
	Operation WatchOperation

	// AbsPath is the absolute file path.
	AbsPath string
}

// WatchOperation indicates the type of file operation.
type WatchOperation string

// WatchOpCreate, WatchOpModify, and WatchOpDelete enumerate the file watch operation types.
const (
	WatchOpCreate WatchOperation = "create"
	WatchOpModify WatchOperation = "modify"
	WatchOpDelete WatchOperation = "delete"
)

// DocWatcher watches for document file changes and emits events.
type DocWatcher struct {
	config     WatchConfig
	sourcesDir string
	watcher    *fsnotify.Watcher
	logger     *slog.Logger
	extensions map[string]bool
	excludes   map[string]bool

	// Debouncing: collect changes before processing
	pendingMu sync.Mutex
	pending   map[string]fsnotify.Op

	// Hash-based change detection
	hashMu sync.RWMutex
	hashes map[string]string

	// Output channel
	events chan WatchEvent

	// Metrics
	droppedEvents atomic.Int64
}

// NewDocWatcher creates a new document file watcher.
func NewDocWatcher(config WatchConfig, sourcesDir string, logger *slog.Logger) (*DocWatcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	if logger == nil {
		logger = slog.Default()
	}

	// Build extension set
	extensions := make(map[string]bool)
	if len(config.FileExtensions) == 0 {
		extensions[".md"] = true
		extensions[".txt"] = true
	} else {
		for _, ext := range config.FileExtensions {
			// Ensure extension starts with .
			if !strings.HasPrefix(ext, ".") {
				ext = "." + ext
			}
			extensions[ext] = true
		}
	}

	// Build exclude set
	excludes := make(map[string]bool)
	if len(config.ExcludeDirs) == 0 {
		excludes[".git"] = true
		excludes["node_modules"] = true
		excludes["vendor"] = true
	} else {
		for _, dir := range config.ExcludeDirs {
			excludes[dir] = true
		}
	}

	return &DocWatcher{
		config:     config,
		sourcesDir: sourcesDir,
		watcher:    fsw,
		logger:     logger,
		extensions: extensions,
		excludes:   excludes,
		pending:    make(map[string]fsnotify.Op),
		hashes:     make(map[string]string),
		events:     make(chan WatchEvent, eventChannelBuffer),
	}, nil
}

// Events returns the channel of watch events.
func (w *DocWatcher) Events() <-chan WatchEvent {
	return w.events
}

// Start begins watching the sources directory for changes.
func (w *DocWatcher) Start(ctx context.Context) error {
	// Create sources directory if it doesn't exist
	if err := os.MkdirAll(w.sourcesDir, 0755); err != nil {
		return err
	}

	// Add watches recursively
	if err := w.addWatchesRecursive(w.sourcesDir); err != nil {
		return err
	}

	// Start the event processing goroutine
	go w.processEvents(ctx)

	w.logger.Info("Document watcher started",
		"sources_dir", w.sourcesDir,
		"debounce", w.config.GetDebounceDelay(),
		"extensions", w.config.FileExtensions)

	return nil
}

// Stop stops the watcher.
// The events channel is closed by processEvents when it exits.
func (w *DocWatcher) Stop() error {
	return w.watcher.Close()
}

// SetHash records the hash for a file (used during initial indexing).
func (w *DocWatcher) SetHash(path, hash string) {
	w.hashMu.Lock()
	defer w.hashMu.Unlock()
	w.hashes[path] = hash
}

// GetHash returns the recorded hash for a file.
func (w *DocWatcher) GetHash(path string) (string, bool) {
	w.hashMu.RLock()
	defer w.hashMu.RUnlock()
	hash, ok := w.hashes[path]
	return hash, ok
}

// addWatchesRecursive adds watches to all directories.
func (w *DocWatcher) addWatchesRecursive(root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Only watch directories
		if !info.IsDir() {
			return nil
		}

		// Skip excluded and hidden directories
		base := filepath.Base(path)
		if w.excludes[base] || (strings.HasPrefix(base, ".") && base != ".") {
			return filepath.SkipDir
		}

		// Add watch
		if err := w.watcher.Add(path); err != nil {
			w.logger.Warn("Failed to watch directory",
				"path", path,
				"error", err)
		} else {
			w.logger.Debug("Watching directory", "path", path)
		}

		return nil
	})
}

// processEvents handles fsnotify events with debouncing.
func (w *DocWatcher) processEvents(ctx context.Context) {
	defer close(w.events) // Close events channel when goroutine exits
	ticker := time.NewTicker(w.config.GetDebounceDelay())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			w.handleFSEvent(event)

		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			w.logger.Error("Watcher error", "error", err)

		case <-ticker.C:
			w.flushPending(ctx)
		}
	}
}

// handleFSEvent processes a single fsnotify event.
func (w *DocWatcher) handleFSEvent(event fsnotify.Event) {
	path := event.Name

	// Check if it's a watched file extension
	ext := strings.ToLower(filepath.Ext(path))
	if !w.extensions[ext] {
		// But handle directory creation (for new watches)
		if event.Has(fsnotify.Create) {
			if info, err := os.Stat(path); err == nil && info.IsDir() {
				w.handleNewDirectory(path)
			}
		}
		return
	}

	// Skip files in excluded directories
	relPath, _ := filepath.Rel(w.sourcesDir, path)
	for excludeDir := range w.excludes {
		if strings.Contains(relPath, excludeDir+string(filepath.Separator)) {
			return
		}
	}

	// Accumulate pending changes
	w.pendingMu.Lock()
	w.pending[path] = event.Op
	w.pendingMu.Unlock()

	w.logger.Debug("Document change detected",
		"path", relPath,
		"op", event.Op.String())
}

// handleNewDirectory adds a watch to a newly created directory.
func (w *DocWatcher) handleNewDirectory(path string) {
	base := filepath.Base(path)
	if w.excludes[base] || strings.HasPrefix(base, ".") {
		return
	}

	if err := w.watcher.Add(path); err != nil {
		w.logger.Warn("Failed to watch new directory",
			"path", path,
			"error", err)
	} else {
		w.logger.Debug("Added watch for new directory", "path", path)
	}
}

// flushPending processes accumulated changes.
func (w *DocWatcher) flushPending(ctx context.Context) {
	w.pendingMu.Lock()
	if len(w.pending) == 0 {
		w.pendingMu.Unlock()
		return
	}

	// Copy and clear pending
	toProcess := make(map[string]fsnotify.Op)
	for k, v := range w.pending {
		toProcess[k] = v
	}
	w.pending = make(map[string]fsnotify.Op)
	w.pendingMu.Unlock()

	// Process each change
	for path, op := range toProcess {
		select {
		case <-ctx.Done():
			return
		default:
		}

		relPath, _ := filepath.Rel(w.sourcesDir, path)
		event := WatchEvent{
			Path:    relPath,
			AbsPath: path,
		}

		if op.Has(fsnotify.Remove) || op.Has(fsnotify.Rename) {
			// File deleted or renamed
			event.Operation = WatchOpDelete

			// Remove from hash cache
			w.hashMu.Lock()
			delete(w.hashes, relPath)
			w.hashMu.Unlock()

			w.sendEvent(event)
			continue
		}

		// Check if file still exists
		if _, err := os.Stat(path); os.IsNotExist(err) {
			event.Operation = WatchOpDelete
			w.sendEvent(event)
			continue
		}

		// Read file and compute hash
		content, err := os.ReadFile(path)
		if err != nil {
			w.logger.Warn("Failed to read file for hash check",
				"path", relPath,
				"error", err)
			continue
		}

		newHash := parser.ContentHash(content)

		// Check if content actually changed
		oldHash, hadHash := w.GetHash(relPath)
		if hadHash && oldHash == newHash {
			// Content unchanged, skip
			continue
		}

		// Update hash cache
		w.SetHash(relPath, newHash)

		if op.Has(fsnotify.Create) || !hadHash {
			event.Operation = WatchOpCreate
		} else {
			event.Operation = WatchOpModify
		}

		w.sendEvent(event)
	}
}

// sendEvent sends an event to the output channel.
func (w *DocWatcher) sendEvent(event WatchEvent) {
	select {
	case w.events <- event:
		w.logger.Debug("Sent watch event",
			"path", event.Path,
			"op", event.Operation)
	default:
		dropped := w.droppedEvents.Add(1)
		w.logger.Warn("Event channel full, dropping event",
			"path", event.Path,
			"total_dropped", dropped)
	}
}

// DroppedEvents returns the number of events dropped due to channel overflow.
func (w *DocWatcher) DroppedEvents() int64 {
	return w.droppedEvents.Load()
}
