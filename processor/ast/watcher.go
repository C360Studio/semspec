package ast

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// WatcherConfig configures the file watcher
type WatcherConfig struct {
	// RepoRoot is the root directory to watch
	RepoRoot string

	// Org is the organization for entity IDs
	Org string

	// Project is the project name for entity IDs
	Project string

	// DebounceDelay is how long to wait for more changes before processing
	DebounceDelay time.Duration

	// Logger for logging events
	Logger *slog.Logger
}

// WatchEvent represents a file change event
type WatchEvent struct {
	// Path is the file path relative to repo root
	Path string

	// Operation is the type of change
	Operation WatchOperation

	// Result is the parse result (nil for delete operations)
	Result *ParseResult

	// Error if parsing failed
	Error error
}

// WatchOperation indicates the type of file operation
type WatchOperation string

const (
	OpCreate WatchOperation = "create"
	OpModify WatchOperation = "modify"
	OpDelete WatchOperation = "delete"
)

// Watcher watches for Go file changes and emits parse results
type Watcher struct {
	config  WatcherConfig
	parser  *Parser
	watcher *fsnotify.Watcher
	logger  *slog.Logger

	// Debouncing: collect changes before processing
	pendingMu sync.Mutex
	pending   map[string]fsnotify.Op // path → most recent operation

	// State tracking for change detection
	hashMu sync.RWMutex
	hashes map[string]string // path → content hash

	// Output channel
	events chan WatchEvent
}

// NewWatcher creates a new file watcher
func NewWatcher(config WatcherConfig) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	logger := config.Logger
	if logger == nil {
		logger = slog.Default()
	}

	debounce := config.DebounceDelay
	if debounce == 0 {
		debounce = 100 * time.Millisecond
	}

	return &Watcher{
		config:  config,
		parser:  NewParser(config.Org, config.Project, config.RepoRoot),
		watcher: fsw,
		logger:  logger,
		pending: make(map[string]fsnotify.Op),
		hashes:  make(map[string]string),
		events:  make(chan WatchEvent, 100),
	}, nil
}

// Events returns the channel of watch events
func (w *Watcher) Events() <-chan WatchEvent {
	return w.events
}

// Start begins watching the repository for changes
func (w *Watcher) Start(ctx context.Context) error {
	// Add watches recursively
	if err := w.addWatchesRecursive(w.config.RepoRoot); err != nil {
		return err
	}

	// Start the event processing goroutine
	go w.processEvents(ctx)

	w.logger.Info("File watcher started",
		"root", w.config.RepoRoot,
		"debounce", w.config.DebounceDelay)

	return nil
}

// Stop stops the watcher
func (w *Watcher) Stop() error {
	close(w.events)
	return w.watcher.Close()
}

// SetHash records the hash for a file (used during initial indexing)
func (w *Watcher) SetHash(path, hash string) {
	w.hashMu.Lock()
	defer w.hashMu.Unlock()
	w.hashes[path] = hash
}

// GetHash returns the recorded hash for a file
func (w *Watcher) GetHash(path string) (string, bool) {
	w.hashMu.RLock()
	defer w.hashMu.RUnlock()
	hash, ok := w.hashes[path]
	return hash, ok
}

// addWatchesRecursive adds watches to all directories
func (w *Watcher) addWatchesRecursive(root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Only watch directories
		if !info.IsDir() {
			return nil
		}

		// Skip vendor and hidden directories
		base := filepath.Base(path)
		if base == "vendor" || strings.HasPrefix(base, ".") {
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

// processEvents handles fsnotify events with debouncing
func (w *Watcher) processEvents(ctx context.Context) {
	ticker := time.NewTicker(w.config.DebounceDelay)
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

// handleFSEvent processes a single fsnotify event
func (w *Watcher) handleFSEvent(event fsnotify.Event) {
	path := event.Name

	// Only care about Go files
	if !strings.HasSuffix(path, ".go") {
		// But handle directory creation (for new watches)
		if event.Has(fsnotify.Create) {
			if info, err := os.Stat(path); err == nil && info.IsDir() {
				w.handleNewDirectory(path)
			}
		}
		return
	}

	// Skip test files if desired (optional - keeping them for now)
	relPath, _ := filepath.Rel(w.config.RepoRoot, path)
	if strings.Contains(relPath, "vendor/") {
		return
	}

	// Accumulate pending changes
	w.pendingMu.Lock()
	w.pending[path] = event.Op
	w.pendingMu.Unlock()

	w.logger.Debug("File change detected",
		"path", relPath,
		"op", event.Op.String())
}

// handleNewDirectory adds a watch to a newly created directory
func (w *Watcher) handleNewDirectory(path string) {
	base := filepath.Base(path)
	if base == "vendor" || strings.HasPrefix(base, ".") {
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

// flushPending processes accumulated changes
func (w *Watcher) flushPending(ctx context.Context) {
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

		relPath, _ := filepath.Rel(w.config.RepoRoot, path)
		event := WatchEvent{Path: relPath}

		if op.Has(fsnotify.Remove) || op.Has(fsnotify.Rename) {
			// File deleted or renamed (treat rename as delete + create)
			event.Operation = OpDelete

			// Remove from hash cache
			w.hashMu.Lock()
			delete(w.hashes, relPath)
			w.hashMu.Unlock()

			w.sendEvent(event)
			continue
		}

		// Check if file still exists
		if _, err := os.Stat(path); os.IsNotExist(err) {
			event.Operation = OpDelete
			w.sendEvent(event)
			continue
		}

		// Parse the file
		result, err := w.parser.ParseFile(ctx, path)
		if err != nil {
			event.Error = err
			w.sendEvent(event)
			continue
		}

		// Check if content actually changed
		oldHash, hadHash := w.GetHash(relPath)
		if hadHash && oldHash == result.Hash {
			// Content unchanged, skip
			continue
		}

		// Update hash cache
		w.SetHash(relPath, result.Hash)

		if op.Has(fsnotify.Create) || !hadHash {
			event.Operation = OpCreate
		} else {
			event.Operation = OpModify
		}
		event.Result = result

		w.sendEvent(event)
	}
}

// sendEvent sends an event to the output channel
func (w *Watcher) sendEvent(event WatchEvent) {
	select {
	case w.events <- event:
		w.logger.Debug("Sent watch event",
			"path", event.Path,
			"op", event.Operation)
	default:
		w.logger.Warn("Event channel full, dropping event",
			"path", event.Path)
	}
}

// IndexDirectory performs initial indexing of all Go files
func (w *Watcher) IndexDirectory(ctx context.Context) ([]*ParseResult, error) {
	results, err := w.parser.ParseDirectory(ctx, w.config.RepoRoot)
	if err != nil {
		return nil, err
	}

	// Record hashes for change detection
	for _, result := range results {
		w.SetHash(result.Path, result.Hash)
	}

	return results, nil
}
