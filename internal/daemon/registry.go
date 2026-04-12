package daemon

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/jeffdhooton/tome/internal/store"
)

// ProjectLayout is the on-disk layout for one indexed project.
type ProjectLayout struct {
	ProjectDir string // absolute path to the project root
	DataDir    string // ~/.tome/projects/<hash>/
	BadgerDir  string // ~/.tome/projects/<hash>/index.db/
}

// ProjectLayoutFor computes the layout for a project.
func ProjectLayoutFor(tomeHome, projectDir string) ProjectLayout {
	h := sha256.Sum256([]byte(projectDir))
	hash := fmt.Sprintf("%x", h[:8])
	dataDir := filepath.Join(tomeHome, "projects", hash)
	return ProjectLayout{
		ProjectDir: projectDir,
		DataDir:    dataDir,
		BadgerDir:  filepath.Join(dataDir, "index.db"),
	}
}

// Registry is a per-daemon cache of opened BadgerDB stores keyed by absolute
// project path.
type Registry struct {
	mu      sync.RWMutex
	entries map[string]*Entry
}

// Entry is one indexed project as known to the daemon.
type Entry struct {
	ProjectDir string
	Layout     ProjectLayout
	Store      *store.Store
}

func NewRegistry() *Registry {
	return &Registry{entries: map[string]*Entry{}}
}

// Get returns the entry for projectDir, opening the store if necessary.
func (r *Registry) Get(tomeHome, projectDir string) (*Entry, error) {
	abs, err := filepath.Abs(projectDir)
	if err != nil {
		return nil, fmt.Errorf("resolve project path: %w", err)
	}
	r.mu.RLock()
	e := r.entries[abs]
	r.mu.RUnlock()
	if e != nil {
		return e, nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if e = r.entries[abs]; e != nil {
		return e, nil
	}
	layout := ProjectLayoutFor(tomeHome, abs)
	if _, err := os.Stat(layout.BadgerDir); errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("project %s is not indexed yet — run `tome init` first", abs)
	} else if err != nil {
		return nil, err
	}
	st, err := store.Open(layout.BadgerDir)
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}
	e = &Entry{ProjectDir: abs, Layout: layout, Store: st}
	r.entries[abs] = e
	return e, nil
}

// Put records an entry that the caller already constructed.
func (r *Registry) Put(e *Entry) {
	r.mu.Lock()
	r.entries[e.ProjectDir] = e
	r.mu.Unlock()
}

// Evict removes one project from the registry, closing its store.
func (r *Registry) Evict(projectDir string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.entries[projectDir]; ok {
		_ = e.Store.Close()
		delete(r.entries, projectDir)
	}
}

// CloseAll closes every store in the registry.
func (r *Registry) CloseAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.entries {
		_ = e.Store.Close()
	}
	r.entries = map[string]*Entry{}
}

// Snapshot returns a copy of the current entries for status reporting.
func (r *Registry) Snapshot() []*Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Entry, 0, len(r.entries))
	for _, e := range r.entries {
		out = append(out, e)
	}
	return out
}
