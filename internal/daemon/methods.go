package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jeffdhooton/tome/internal/rpc"
	"github.com/jeffdhooton/tome/internal/schema"
	"github.com/jeffdhooton/tome/internal/sources/mysql"
	"github.com/jeffdhooton/tome/internal/sources/postgres"
	"github.com/jeffdhooton/tome/internal/store"
)

func (d *Daemon) tomeHome() string { return d.layout.Home }

// registerMethods wires every supported RPC method into the server.
func (d *Daemon) registerMethods() {
	d.server.Register("init", d.handleInit)
	d.server.Register("describe", d.handleDescribe)
	d.server.Register("relations", d.handleRelations)
	d.server.Register("search", d.handleSearch)
	d.server.Register("enums", d.handleEnums)
	d.server.Register("refresh", d.handleRefresh)
	d.server.Register("status", d.handleStatus)
	d.server.Register("shutdown", d.handleShutdown)
	d.server.Register("ping", func(_ context.Context, _ json.RawMessage) (any, error) {
		return map[string]any{"ok": true, "pid": os.Getpid()}, nil
	})
}

// InitParams instructs the daemon to connect to a DB and index its schema.
type InitParams struct {
	Project   string `json:"project"`
	DSN       string `json:"dsn"`
	DetectEnv bool   `json:"detect_env"`
}

// InitResult is returned after a successful init.
type InitResult struct {
	Project    string `json:"project"`
	DBType     string `json:"db_type"`
	DBName     string `json:"db_name"`
	TableCount int    `json:"table_count"`
	IndexedAt  string `json:"indexed_at"`
	ElapsedMs  int64  `json:"elapsed_ms"`
}

func (d *Daemon) handleInit(_ context.Context, raw json.RawMessage) (any, error) {
	var p InitParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &rpc.Error{Code: rpc.CodeInvalidParams, Message: err.Error()}
	}
	if p.Project == "" {
		return nil, &rpc.Error{Code: rpc.CodeInvalidParams, Message: "project is required"}
	}

	dsn := p.DSN
	if dsn == "" && p.DetectEnv {
		detected, err := schema.DetectDSNFromEnv(p.Project)
		if err != nil {
			return nil, fmt.Errorf("detect DSN from .env: %w", err)
		}
		dsn = detected
	}
	if dsn == "" {
		return nil, &rpc.Error{Code: rpc.CodeInvalidParams, Message: "dsn is required (pass --dsn or use --detect-env)"}
	}

	// Evict any existing store for this project before reindexing.
	d.registry.Evict(p.Project)

	start := time.Now()

	// Parse DSN and create introspector.
	dbType, _, err := schema.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse DSN: %w", err)
	}
	var introspector schema.Introspector
	switch dbType {
	case "mysql":
		introspector = mysql.New()
	case "postgres":
		introspector = postgres.New()
	default:
		return nil, fmt.Errorf("unsupported database type: %s", dbType)
	}
	defer introspector.Close()

	if err := introspector.Connect(dsn); err != nil {
		return nil, fmt.Errorf("connect to database: %w", err)
	}

	snapshot, err := introspector.Introspect()
	if err != nil {
		return nil, fmt.Errorf("introspect database: %w", err)
	}

	// Write to store.
	layout := ProjectLayoutFor(d.tomeHome(), p.Project)
	if err := os.MkdirAll(layout.DataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create project dir: %w", err)
	}

	st, err := store.Open(layout.BadgerDir)
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}

	// Check schema version and reset if needed.
	ver, err := st.SchemaVersionOnDisk()
	if err == nil && ver != 0 && ver != store.SchemaVersion {
		if err := st.Reset(); err != nil {
			_ = st.Close()
			return nil, fmt.Errorf("reset store: %w", err)
		}
	}

	if err := writeSnapshot(st, snapshot, dsn, p.Project); err != nil {
		_ = st.Close()
		return nil, fmt.Errorf("write snapshot: %w", err)
	}

	d.registry.Put(&Entry{ProjectDir: p.Project, Layout: layout, Store: st})

	indexedAt := time.Now().UTC().Format(time.RFC3339)
	return &InitResult{
		Project:    p.Project,
		DBType:     snapshot.DBType,
		DBName:     snapshot.DBName,
		TableCount: len(snapshot.Tables),
		IndexedAt:  indexedAt,
		ElapsedMs:  time.Since(start).Milliseconds(),
	}, nil
}

// writeSnapshot writes a SchemaSnapshot into the store.
func writeSnapshot(st *store.Store, snap *schema.SchemaSnapshot, dsn, projectDir string) error {
	if err := st.Reset(); err != nil {
		return err
	}

	// Write metadata.
	if err := st.SetMeta("schema_version", store.SchemaVersion); err != nil {
		return err
	}
	if err := st.SetMeta("dsn", dsn); err != nil {
		return err
	}
	if err := st.SetMeta("db_type", snap.DBType); err != nil {
		return err
	}
	if err := st.SetMeta("db_name", snap.DBName); err != nil {
		return err
	}
	if err := st.SetMeta("project_dir", projectDir); err != nil {
		return err
	}
	if err := st.SetMeta("indexed_at", time.Now().UTC().Format(time.RFC3339)); err != nil {
		return err
	}

	w := st.NewWriter()

	// Build a reverse FK map: referenced_table -> []ForeignKeyRecord
	reverseFKs := map[string][]schema.ForeignKeyRecord{}
	for _, t := range snap.Tables {
		for _, fk := range t.ForeignKeys {
			reverseFKs[fk.ReferencedTable] = append(reverseFKs[fk.ReferencedTable], fk)
		}
	}

	for _, t := range snap.Tables {
		// Fill in ReferencedBy from the reverse map.
		t.ReferencedBy = reverseFKs[t.Name]

		data, err := json.Marshal(t)
		if err != nil {
			return fmt.Errorf("marshal table %s: %w", t.Name, err)
		}
		if err := w.PutTable(t.Name, data); err != nil {
			return err
		}

		// Search index: table name.
		if err := w.PutName(t.Name, t.Name); err != nil {
			return err
		}

		// Search index: column names.
		for _, col := range t.Columns {
			if err := w.PutName(col.Name, t.Name+"."+col.Name); err != nil {
				return err
			}
		}

		// Reverse FK index.
		for _, fk := range t.ForeignKeys {
			fkData, err := json.Marshal(fk)
			if err != nil {
				return err
			}
			if err := w.PutFKRef(fk.ReferencedTable, t.Name, fk.Column, fkData); err != nil {
				return err
			}
		}
	}

	// Enum records.
	for _, e := range snap.Enums {
		data, err := json.Marshal(e)
		if err != nil {
			return err
		}
		if err := w.PutEnum(e.Table, e.Column, data); err != nil {
			return err
		}
	}

	return w.Flush()
}

// DescribeParams requests a table description.
type DescribeParams struct {
	Project string `json:"project"`
	Table   string `json:"table"`
}

func (d *Daemon) handleDescribe(_ context.Context, raw json.RawMessage) (any, error) {
	var p DescribeParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &rpc.Error{Code: rpc.CodeInvalidParams, Message: err.Error()}
	}
	if p.Project == "" || p.Table == "" {
		return nil, &rpc.Error{Code: rpc.CodeInvalidParams, Message: "project and table are required"}
	}
	entry, err := d.registry.Get(d.tomeHome(), p.Project)
	if err != nil {
		return nil, err
	}
	data, err := entry.Store.GetTable(p.Table)
	if err != nil {
		return nil, err
	}
	var table schema.TableRecord
	if err := json.Unmarshal(data, &table); err != nil {
		return nil, err
	}
	return table, nil
}

// RelationsParams requests FK relationships for a table.
type RelationsParams struct {
	Project string `json:"project"`
	Table   string `json:"table"`
}

// RelationsResult is the FK graph for one table.
type RelationsResult struct {
	Table    string                  `json:"table"`
	Outgoing []schema.ForeignKeyRecord `json:"outgoing"`
	Incoming []schema.ForeignKeyRecord `json:"incoming"`
}

func (d *Daemon) handleRelations(_ context.Context, raw json.RawMessage) (any, error) {
	var p RelationsParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &rpc.Error{Code: rpc.CodeInvalidParams, Message: err.Error()}
	}
	if p.Project == "" || p.Table == "" {
		return nil, &rpc.Error{Code: rpc.CodeInvalidParams, Message: "project and table are required"}
	}
	entry, err := d.registry.Get(d.tomeHome(), p.Project)
	if err != nil {
		return nil, err
	}

	// Get outgoing FKs from the table record.
	data, err := entry.Store.GetTable(p.Table)
	if err != nil {
		return nil, err
	}
	var table schema.TableRecord
	if err := json.Unmarshal(data, &table); err != nil {
		return nil, err
	}

	// Get incoming FKs from the reverse index.
	incomingRaw, err := entry.Store.GetForeignKeysTo(p.Table)
	if err != nil {
		return nil, err
	}
	var incoming []schema.ForeignKeyRecord
	for _, raw := range incomingRaw {
		var fk schema.ForeignKeyRecord
		if err := json.Unmarshal(raw, &fk); err != nil {
			continue
		}
		incoming = append(incoming, fk)
	}

	return &RelationsResult{
		Table:    p.Table,
		Outgoing: table.ForeignKeys,
		Incoming: incoming,
	}, nil
}

// SearchParams searches tables/columns by name.
type SearchParams struct {
	Project string `json:"project"`
	Query   string `json:"query"`
}

// SearchResult contains search matches.
type SearchResult struct {
	Query   string        `json:"query"`
	Matches []SearchMatch `json:"matches"`
}

// SearchMatch is one search hit.
type SearchMatch struct {
	Type  string `json:"type"` // "table" or "column"
	Name  string `json:"name"`
	Table string `json:"table,omitempty"`
}

func (d *Daemon) handleSearch(_ context.Context, raw json.RawMessage) (any, error) {
	var p SearchParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &rpc.Error{Code: rpc.CodeInvalidParams, Message: err.Error()}
	}
	if p.Project == "" || p.Query == "" {
		return nil, &rpc.Error{Code: rpc.CodeInvalidParams, Message: "project and query are required"}
	}
	entry, err := d.registry.Get(d.tomeHome(), p.Project)
	if err != nil {
		return nil, err
	}
	rawMatches, err := entry.Store.SearchByName(p.Query)
	if err != nil {
		return nil, err
	}

	var matches []SearchMatch
	for _, m := range rawMatches {
		// Format: token:table or token:table.column
		parts := splitSearchMatch(m)
		matches = append(matches, parts)
	}

	return &SearchResult{
		Query:   p.Query,
		Matches: matches,
	}, nil
}

// splitSearchMatch parses a "token:context" string from the search index.
func splitSearchMatch(s string) SearchMatch {
	// name index format: <token>:<table> or <token>:<table>.<column>
	idx := -1
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			idx = i
			break
		}
	}
	if idx < 0 {
		return SearchMatch{Type: "table", Name: s}
	}
	context := s[idx+1:]
	// Check if context contains a dot (table.column)
	dotIdx := -1
	for i := 0; i < len(context); i++ {
		if context[i] == '.' {
			dotIdx = i
			break
		}
	}
	if dotIdx >= 0 {
		return SearchMatch{
			Type:  "column",
			Name:  context[dotIdx+1:],
			Table: context[:dotIdx],
		}
	}
	return SearchMatch{Type: "table", Name: context}
}

// EnumsParams requests enum values.
type EnumsParams struct {
	Project string `json:"project"`
	Table   string `json:"table"`
	Column  string `json:"column"`
}

func (d *Daemon) handleEnums(_ context.Context, raw json.RawMessage) (any, error) {
	var p EnumsParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &rpc.Error{Code: rpc.CodeInvalidParams, Message: err.Error()}
	}
	if p.Project == "" {
		return nil, &rpc.Error{Code: rpc.CodeInvalidParams, Message: "project is required"}
	}
	entry, err := d.registry.Get(d.tomeHome(), p.Project)
	if err != nil {
		return nil, err
	}
	rawEnums, err := entry.Store.GetEnums(p.Table, p.Column)
	if err != nil {
		return nil, err
	}
	var enums []schema.EnumRecord
	for _, raw := range rawEnums {
		var e schema.EnumRecord
		if err := json.Unmarshal(raw, &e); err != nil {
			continue
		}
		enums = append(enums, e)
	}
	return map[string]any{"enums": enums}, nil
}

func (d *Daemon) handleRefresh(ctx context.Context, raw json.RawMessage) (any, error) {
	// Refresh re-reads the DSN from the store and re-introspects.
	var p struct {
		Project string `json:"project"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &rpc.Error{Code: rpc.CodeInvalidParams, Message: err.Error()}
	}
	if p.Project == "" {
		return nil, &rpc.Error{Code: rpc.CodeInvalidParams, Message: "project is required"}
	}

	// Read the stored DSN.
	entry, err := d.registry.Get(d.tomeHome(), p.Project)
	if err != nil {
		return nil, err
	}
	dsnBytes, err := entry.Store.GetMeta("dsn")
	if err != nil {
		return nil, fmt.Errorf("read stored DSN: %w", err)
	}
	var dsn string
	if err := json.Unmarshal(dsnBytes, &dsn); err != nil {
		return nil, fmt.Errorf("decode DSN: %w", err)
	}

	// Re-init with the stored DSN.
	initParams, _ := json.Marshal(InitParams{
		Project: p.Project,
		DSN:     dsn,
	})
	return d.handleInit(ctx, initParams)
}

// StatusResult is the daemon's view of the world.
type StatusResult struct {
	PID      int                   `json:"pid"`
	Projects []*ProjectStatusEntry `json:"projects"`
}

// ProjectStatusEntry is one indexed project.
type ProjectStatusEntry struct {
	Project    string `json:"project"`
	DBType     string `json:"db_type"`
	DBName     string `json:"db_name"`
	TableCount int    `json:"table_count"`
	IndexedAt  string `json:"indexed_at"`
}

func (d *Daemon) handleStatus(_ context.Context, _ json.RawMessage) (any, error) {
	res := &StatusResult{PID: os.Getpid()}

	// Check in-memory registry.
	for _, e := range d.registry.Snapshot() {
		entry := &ProjectStatusEntry{Project: e.ProjectDir}
		if b, err := e.Store.GetMeta("db_type"); err == nil {
			_ = json.Unmarshal(b, &entry.DBType)
		}
		if b, err := e.Store.GetMeta("db_name"); err == nil {
			_ = json.Unmarshal(b, &entry.DBName)
		}
		if b, err := e.Store.GetMeta("indexed_at"); err == nil {
			_ = json.Unmarshal(b, &entry.IndexedAt)
		}
		if tables, err := e.Store.ListTables(); err == nil {
			entry.TableCount = len(tables)
		}
		res.Projects = append(res.Projects, entry)
	}

	// Also scan disk for projects not in memory.
	seen := map[string]bool{}
	for _, p := range res.Projects {
		seen[p.Project] = true
	}
	projectsDir := filepath.Join(d.tomeHome(), "projects")
	entries, _ := os.ReadDir(projectsDir)
	for _, ent := range entries {
		badgerDir := filepath.Join(projectsDir, ent.Name(), "index.db")
		if _, err := os.Stat(badgerDir); err != nil {
			continue
		}
		st, err := store.Open(badgerDir)
		if err != nil {
			continue
		}
		entry := &ProjectStatusEntry{}
		if b, err := st.GetMeta("project_dir"); err == nil {
			_ = json.Unmarshal(b, &entry.Project)
		}
		if seen[entry.Project] {
			_ = st.Close()
			continue
		}
		if b, err := st.GetMeta("db_type"); err == nil {
			_ = json.Unmarshal(b, &entry.DBType)
		}
		if b, err := st.GetMeta("db_name"); err == nil {
			_ = json.Unmarshal(b, &entry.DBName)
		}
		if b, err := st.GetMeta("indexed_at"); err == nil {
			_ = json.Unmarshal(b, &entry.IndexedAt)
		}
		if tables, err := st.ListTables(); err == nil {
			entry.TableCount = len(tables)
		}
		_ = st.Close()
		res.Projects = append(res.Projects, entry)
	}

	return res, nil
}

func (d *Daemon) handleShutdown(_ context.Context, _ json.RawMessage) (any, error) {
	go func() {
		time.Sleep(50 * time.Millisecond)
		d.mu.Lock()
		ln := d.listener
		d.mu.Unlock()
		if ln != nil {
			_ = ln.Close()
		}
	}()
	return map[string]any{"ok": true}, nil
}
