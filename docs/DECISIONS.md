# tome — architectural decisions

This document records every significant decision made during tome's development.
Format mirrors scry's DECISIONS.md: verdict + reasoning, not just the verdict.

---

## 1. Architecture: daemon over Unix socket (same as scry)

**Verdict:** One long-running daemon per user, auto-spawned on first CLI call,
communicating via JSON-RPC 2.0 over a Unix socket at `~/.tome/tomed.sock`.

**Why:** Proven pattern from scry. Amortizes BadgerDB open cost across queries.
The daemon stays warm between calls so `tome describe users` returns in <10ms
instead of the ~15ms cold-start of opening BadgerDB on every invocation.

---

## 2. Database credentials: explicit DSN with optional .env detection

**Verdict:** `tome init --dsn "mysql://user:pass@localhost/mydb"` is the primary
path. `--detect-env` reads `.env` for `DATABASE_URL`, `DATABASE_DSN`, or
Laravel-style `DB_CONNECTION`+`DB_HOST`+`DB_PORT`+`DB_DATABASE`+`DB_USERNAME`+`DB_PASSWORD`.

**Why:**
- Explicit DSN avoids framework-specific config parsing (which would mean parsing
  PHP, YAML, TOML, etc.).
- `.env` detection covers the 90% case for Laravel projects (which always have
  a `.env` with `DB_*` vars) without any framework-specific logic.
- DSN is stored in the BadgerDB store so `tome refresh` can re-introspect without
  re-entering credentials. Store is under `~/.tome/projects/<hash>/` with 0700
  directory permissions — same trust model as the user's `.env` file.

**Alternatives considered:**
- Auto-detect from `config/database.php`: requires parsing PHP, pulls in a PHP
  interpreter or regex heuristics. Too fragile for P0.
- Prompt interactively: bad for AI agent use case (no TTY).

---

## 3. Index strategy: introspect on init, cache in BadgerDB

**Verdict:** `tome init` connects to the live database, reads
`INFORMATION_SCHEMA` (MySQL) or `pg_catalog` (PostgreSQL), and writes the full
schema snapshot to BadgerDB. No file watching, no automatic re-introspection.
`tome refresh` manually re-introspects.

**Why:** Database schemas change much less frequently than source code. A
Laravel project might run 2-3 migrations per week. Re-introspecting on every
query (simple but slow) would add ~200-500ms per call. Caching and manual
refresh gives <10ms queries with a one-time ~1-3s init cost.

**No file watching:** Unlike scry (which watches source files for changes),
tome doesn't need to watch anything. The schema lives in the database, not in
files. Migration file watching would be unreliable (you'd have to detect which
migrations have actually been applied). Manual refresh is the right answer here.

---

## 4. ORM parsing: deferred to P1

**Verdict:** P0 only does database introspection. No Eloquent model parsing,
no Prisma schema parsing, no TypeORM entity parsing.

**Why:** Database introspection gives you 80% of what you need: column names,
types, indexes, foreign keys, enum values. The remaining 20% (casts,
accessors, mutators, validation rules, relationship method names) comes from
ORM models — but parsing those requires framework-specific logic that's
complex and fragile. Better to ship the 80% now and layer ORM enrichment later.

---

## 5. Storage layout: per-project BadgerDB under ~/.tome/projects/

**Verdict:** `~/.tome/projects/<sha256[:16]>/index.db/` — one BadgerDB per
project, keyed by SHA256 hash of the absolute project directory path.

**Why:** Same pattern as scry (`~/.scry/repos/<hash>/index.db/`). Hash-based
directory names avoid filesystem issues with long paths or special characters.
One store per project keeps data isolated and allows easy cleanup.

---

## 6. DSN format normalization

**Verdict:** Accept URI format (`mysql://user:pass@host:port/db`) and convert
to driver-native format internally. MySQL's go-sql-driver expects
`user:pass@tcp(host:port)/db`, so we normalize at parse time.

**Why:** Users will naturally type URIs (that's what DATABASE_URL uses, what
Docker prints, what every DB GUI shows). The conversion is deterministic and
well-defined. PostgreSQL's pgx driver already accepts URI format natively.

---

## 7. Pure-Go database drivers (no CGO)

**Verdict:**
- MySQL: `github.com/go-sql-driver/mysql` (pure Go)
- PostgreSQL: `github.com/jackc/pgx/v5` (pure Go)

**Why:** Hard constraint inherited from scry: single static binary, no CGO,
cross-compiles freely. Both drivers are production-grade and widely used.
pgx is the de facto standard pure-Go PostgreSQL driver.

---

## 8. MCP tool design: 4 tools for P0

**Verdict:** `tome_describe`, `tome_relations`, `tome_search`, `tome_enums`.

**Why:** These map directly to the four most common schema questions an AI
agent asks:
1. "What columns does this table have?" → `tome_describe`
2. "What references this table?" → `tome_relations`
3. "Find tables/columns matching X" → `tome_search`
4. "What are the valid enum values?" → `tome_enums`

Each tool maps to one daemon RPC method. The MCP server is stateless — opens
one connection per tool call, same as scry.

---

## 9. Search index: simple name-based prefix scan

**Verdict:** Store `name:<lowercase_token>:<context>` keys in BadgerDB. Search
does a prefix scan over all `name:` keys looking for substring matches.

**Why:** This is simple and fast enough for schema search. A typical database
has 50-500 tables, each with 5-20 columns — so the total search space is
250-10,000 entries. A full prefix scan over that takes <1ms in BadgerDB.
No need for a proper full-text search engine.

---

## 10. Reverse FK index for relations queries

**Verdict:** Store `fk_ref:<referenced_table>:<src_table>:<column>` keys so
`tome relations users` can find all tables that reference `users` without
scanning every table's FK list.

**Why:** Without the reverse index, answering "what tables reference users"
requires loading every table record and scanning its FK list. With the index,
it's a single prefix scan on `fk_ref:users:`. The write cost is negligible
since FKs are written once during init.
