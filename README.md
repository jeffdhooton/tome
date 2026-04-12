# tome

**Schema awareness daemon for AI agents.** Pre-indexes your database schema (tables, columns, types, indexes, foreign keys, enums) and serves it as millisecond-latency MCP queries. Replaces the 3-6 file reads Claude Code does every time it needs to answer "what columns does this table have."

> **Status:** P0 shipped. Single static Go binary. MySQL and PostgreSQL introspection, daemon mode with auto-spawn, JSON-RPC over Unix socket, MCP stdio server with 4 tools. See [`docs/DECISIONS.md`](docs/DECISIONS.md) for architectural decisions.

---

## The problem

When Claude Code needs to understand a table's schema, it:

1. Greps migration files to find the table definition
2. Reads the Eloquent model / Prisma schema / ActiveRecord model
3. Checks factory definitions for field types
4. Maybe reads a form request for validation rules
5. Maybe reads an API resource/transformer for response shape

That's 3-6 tool calls and thousands of tokens for one schema question, and it happens constantly during feature work. `tome describe users` answers it in one call.

## Install

**From source** (requires Go 1.23+):

```bash
go install github.com/jeffdhooton/tome/cmd/tome@latest
```

**Post-install setup:**

```bash
tome setup        # installs the Claude Code skill + MCP server registration
tome doctor       # checks every prereq and prints a status checklist
```

## Quick start

```bash
# Index a database. The daemon auto-spawns on first call.
cd ~/path/to/your/project
tome init --dsn "mysql://root:secret@localhost:3306/myapp"

# Or auto-detect from .env (works with DATABASE_URL or Laravel DB_* vars)
tome init --detect-env

# Describe a table
tome describe users --pretty

# Find what references a table
tome relations users --pretty

# Search for tables/columns by name
tome search email --pretty

# List enum values
tome enums orders.status --pretty

# Daemon control
tome status                      # what projects are indexed?
tome refresh                     # re-introspect the database
tome start                       # explicit start (auto-spawned otherwise)
tome stop                        # graceful shutdown

# Claude Code integration
tome setup                       # install skill + MCP server (idempotent)
tome mcp                         # MCP stdio server (launched by Claude Code)
```

Output is JSON by default. Pass `--pretty` for human reading.

## What works today

| Feature | Status |
|---|---|
| **Databases** | MySQL (INFORMATION_SCHEMA), PostgreSQL (pg_catalog) |
| **Daemon** | Auto-spawned on first CLI call, Unix socket at `~/.tome/tomed.sock` |
| **JSON-RPC 2.0** | Newline-delimited over Unix socket; methods mirror CLI subcommands |
| **Commands** | `init`, `describe`, `relations`, `search`, `enums`, `refresh`, `status`, `start`, `stop` |
| **Schema cache** | BadgerDB per project at `~/.tome/projects/<sha256[:16]>/`, schema-versioned |
| **DSN detection** | Explicit `--dsn` flag, or `--detect-env` reads `.env` for `DATABASE_URL` / Laravel `DB_*` vars |
| **Enum extraction** | MySQL `enum()`/`set()` from COLUMN_TYPE, PostgreSQL `pg_enum` types |
| **FK graph** | Forward and reverse foreign key indexes for instant relationship queries |
| **Claude Code** | `tome setup` installs a skill + registers as User-scope MCP server. 4 tools: `tome_describe`, `tome_relations`, `tome_search`, `tome_enums` |

## MCP tools

When registered with Claude Code, tome exposes four tools:

| Tool | Use case |
|---|---|
| `tome_describe` | "What columns does users have?" — full table schema in one call |
| `tome_relations` | "What tables reference orders?" — FK graph, inbound and outbound |
| `tome_search` | "Find tables with an email column" — name substring search |
| `tome_enums` | "What are the valid order statuses?" — enum/set column values |

## DSN formats

tome accepts standard URI formats and auto-detects the database type:

```
mysql://user:pass@localhost:3306/mydb
postgres://user:pass@localhost:5432/mydb
postgresql://user:pass@localhost:5432/mydb
```

The `--detect-env` flag reads your project's `.env` file and looks for:

- `DATABASE_URL` (universal)
- `DATABASE_DSN`
- `DB_CONNECTION` + `DB_HOST` + `DB_PORT` + `DB_DATABASE` + `DB_USERNAME` + `DB_PASSWORD` (Laravel)

## Architecture

```
┌──────────────────────────────────────────────────────────┐
│                       tome CLI                           │
│  tome init | describe | relations | search | enums ...  │
└───────────────────────┬──────────────────────────────────┘
                        │ JSON-RPC 2.0 (newline-delimited JSON)
                        │ ~/.tome/tomed.sock
                        ▼
┌──────────────────────────────────────────────────────────┐
│                  tome start --foreground                  │
│   ┌──────────────────────────────────────────────────┐   │
│   │          JSON-RPC dispatcher (rpc.Server)        │   │
│   └──────────────────────┬───────────────────────────┘   │
│                          │                               │
│   ┌──────────────────────▼───────────────────────────┐   │
│   │              RPC Handlers (daemon/methods.go)     │   │
│   │   init | describe | relations | search | enums   │   │
│   └────────┬─────────────────────────┬───────────────┘   │
│            │                         │                   │
│   ┌────────▼────────┐    ┌──────────▼──────────┐        │
│   │  Store Registry │    │   DB Introspection  │        │
│   │  (one BadgerDB  │    │   ┌───────┐ ┌────┐ │        │
│   │   per project)  │    │   │ MySQL │ │ PG │ │        │
│   └─────────────────┘    │   └───────┘ └────┘ │        │
│                          └─────────────────────┘        │
└──────────────────────────────────────────────────────────┘
```

## Layout

```
tome/
├── cmd/tome/                  # cobra CLI; one binary, daemon and client
│   ├── main.go                # root command, version, subcommand wiring
│   ├── daemon.go              # client-side auto-spawn helpers
│   ├── start.go / stop.go     # daemon lifecycle
│   ├── init.go                # tome init --dsn --detect-env
│   ├── describe.go            # tome describe <table>
│   ├── relations.go           # tome relations <table>
│   ├── search.go              # tome search <term>
│   ├── enums.go               # tome enums [table.column]
│   ├── refresh.go             # tome refresh
│   ├── status.go              # tome status
│   ├── mcp.go                 # MCP stdio server entry
│   ├── setup.go               # Claude Code integration
│   └── doctor.go              # environment diagnostics
├── internal/
│   ├── rpc/                   # JSON-RPC 2.0 over Unix socket (server + client)
│   ├── daemon/                # daemon lifecycle, registry, RPC handlers
│   ├── store/                 # BadgerDB-backed schema cache
│   ├── schema/                # types, DSN parsing, .env detection
│   ├── sources/
│   │   ├── mysql/             # INFORMATION_SCHEMA introspection
│   │   └── postgres/          # pg_catalog introspection
│   ├── mcp/                   # MCP stdio server (initialize, tools/list, tools/call)
│   ├── setup/                 # SKILL.md embed + claude mcp add
│   └── doctor/                # read-only health checks
└── docs/
    └── DECISIONS.md           # architectural decisions log
```

## Hard constraints

- **Go 1.23+.** Single static binary, no CGO.
- **Pure-Go drivers.** `go-sql-driver/mysql` for MySQL, `jackc/pgx/v5` for PostgreSQL.
- **No telemetry, no network calls** except to the local database being introspected.
- **JSON output by default.** Primary user is an AI agent. `--pretty` for humans.
- **Local-only.** No cloud, no shared state. One daemon per user.

## Part of the agent tool suite

A collection of local-first, single-binary dev tools built for AI coding agents. All share the same architecture: Go, no CGO, BadgerDB, daemon over Unix socket, MCP stdio, millisecond-latency queries. Free, local-only, no cloud.

| Tool | What it does | Status |
|------|-------------|--------|
| **[scry](https://github.com/jeffdhooton/scry)** | Code intelligence — symbols, refs, call graphs, impls, test coverage | Shipped |
| **[flume](https://github.com/jeffdhooton/flume)** | Runtime visibility — HTTP requests, SQL queries, exceptions from dev servers | In progress |
| **[tome](https://github.com/jeffdhooton/tome)** | Schema awareness — DB schemas, API shapes, ORM models, enums | In progress |
| **[lore](https://github.com/jeffdhooton/lore)** | Git intelligence — blame, history, co-change patterns, hotspots | In progress |
