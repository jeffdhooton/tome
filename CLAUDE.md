# tome — instructions for the building agent

This file is loaded automatically by Claude Code in this directory. Read it first.

## What this is

A local schema and data-contract awareness daemon for AI agents. Pre-indexes database schemas, API response shapes, config structures, and enum values, then serves them as millisecond-latency MCP queries. Replaces the 3-6 file reads CC does every time it needs to answer "what columns does this table have."

## The problem

When Claude Code needs to understand data shapes today, it:
1. Greps migration files to find the table definition
2. Reads the Eloquent model / Prisma schema / ActiveRecord model
3. Checks factory definitions for field types
4. Maybe reads a form request for validation rules
5. Maybe reads an API resource/transformer for response shape

That's 3-6 tool calls for one schema question, and it happens constantly during feature work. `tome describe users` should answer it in one call.

## How it should work

tome connects to data sources and indexes their schemas:

- **Database schemas** — connect to the actual database (MySQL, PostgreSQL, SQLite), introspect tables, columns, types, indexes, foreign keys, constraints
- **API contracts** — parse OpenAPI/Swagger specs, or ingest sniffed response shapes from flume (sibling project)
- **ORM models** — parse Eloquent models (casts, relationships, fillable/guarded), Prisma schemas, TypeORM entities
- **Config structures** — Laravel config files, .env schemas, TypeScript config types
- **Enum values** — PHP enums, TS enums, database enum columns

Queryable via MCP tools like:

- `tome_describe` — "describe the users table" (columns, types, indexes, relationships)
- `tome_relations` — "what tables reference users" (foreign keys, ORM relationships)
- `tome_endpoints` — "what does POST /api/orders accept and return"
- `tome_enums` — "what are the valid values for order_status"
- `tome_search` — "find tables/fields matching 'email'"

## Hard constraints (inherited from the sibling projects)

- **Language: Go 1.23+.** Same stack as `~/workspace/scry` and `~/workspace/trawl`.
- **No CGO. Ever.** Single static binary. This means `modernc.org/sqlite` if you need to read SQLite databases, and pure-Go MySQL/PostgreSQL drivers.
- **No telemetry, no network calls** except to the local database being introspected.
- **JSON output by default.** Primary user is an AI agent. Add `--pretty` for humans.
- **Local-only.** No cloud, no shared state.
- **One binary, one daemon per user.** Auto-spawn on first CLI call.
- **MCP stdio server** for Claude Code integration.

## Sibling projects — borrow decisions wholesale

- **`~/workspace/scry`** — code intelligence daemon. Same architecture: Go, cobra CLI, BadgerDB, daemon over Unix socket, JSON-RPC 2.0, MCP stdio, GoReleaser releases, `doctor` command, `setup` command. **Read scry's CLAUDE.md, README.md, and docs/DECISIONS.md for patterns to copy.**
- **`~/workspace/trawl`** — web scraping daemon. Same stack.
- **`~/workspace/flume`** — runtime visibility (sibling being built in parallel). flume can feed sniffed API response shapes into tome for automatic contract discovery.
- **`~/workspace/lore`** — git intelligence (sibling being built in parallel).

## Language/framework support priority

1. **PHP/Laravel + MySQL** — primary user stack. Eloquent models, Laravel migrations, MySQL introspection.
2. **TypeScript + PostgreSQL** — Prisma schemas, TypeORM, Drizzle, PostgreSQL introspection.
3. **Go + PostgreSQL/SQLite** — struct tags, sqlc definitions.

## Key design decisions to make (and document in docs/DECISIONS.md)

1. **Database connection**: how does tome learn the DB credentials? Read .env? Accept a DSN on `tome init`? Auto-detect from framework config?
2. **Index strategy**: introspect live DB on `tome init` and cache the schema? Or re-introspect on every query (simpler but slower)?
3. **ORM parsing**: how deep to go on framework-specific model parsing vs. just relying on DB introspection? DB gives you columns/types/FKs. ORM gives you casts, accessors, relationships, validation rules.
4. **Freshness**: schemas change less often than code. How to detect when to re-index? Watch migration files? Periodic re-introspect?

## What "done" looks like for P0

- `tome init` connects to a local database (MySQL or PostgreSQL) and indexes the schema
- `tome describe <table>` returns columns, types, indexes, foreign keys
- `tome relations <table>` returns tables that reference or are referenced by the target
- `tome mcp` exposes MCP tools for querying schema data
- `tome setup` registers the MCP server with Claude Code
- CC can query "what columns does the users table have" and get an instant, complete answer

## When you make decisions

Document every architectural choice in `docs/DECISIONS.md`. Include reasoning, not just the verdict. Same format as scry's DECISIONS.md.
