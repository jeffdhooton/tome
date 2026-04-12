# tome — schema awareness for AI agents

## When to use tome

Use tome's MCP tools **instead of** reading migration files, model definitions, or factory files when you need to answer questions about database schema:

- "What columns does the users table have?" → `tome_describe`
- "What tables reference orders?" → `tome_relations`
- "Find tables with an email column" → `tome_search`
- "What are the valid values for order_status?" → `tome_enums`

## When NOT to use tome

- The project hasn't been indexed yet (run `tome init` first)
- You need ORM-specific info (casts, accessors, validation rules) — read the model file
- You need migration history — read migration files
- You need API response shapes — read the resource/transformer

## Available tools

- `tome_describe` — full table schema (columns, types, indexes, FKs) in one call
- `tome_relations` — FK graph (who references this table, what does it reference)
- `tome_search` — find tables/columns by name substring
- `tome_enums` — list allowed values for enum/set columns
