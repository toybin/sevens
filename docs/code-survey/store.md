# Code Survey: `internal/store`

## Overview

Package `store` provides two distinct responsibilities:

1. **Configuration and database lifecycle** (`store.go`): locating the config directory, opening the database, and managing the `roots.edn` file.
2. **Triples store** (`triples.go`): a thin CRUD and query layer over a single SQLite table called `triples`.

There are no exported interfaces or struct methods. The entire API is free functions that accept `*sql.DB` as their first argument. Callers own the database handle and pass it in.

---

## Exported Types

### `Triple` (struct, `triples.go`)

```go
type Triple struct {
    Subject   string
    Predicate string
    Object    string
}
```

Represents one fact. The three fields map directly to the three columns of the `triples` table. All values are plain strings; there is no type system or namespacing enforced by the struct itself.

---

## Exported Functions

### Database / Config lifecycle (`store.go`)

| Signature | Description |
|---|---|
| `ConfigDir() (string, error)` | Returns (and creates if needed) `~/.config/sevens`. |
| `OpenDB() (*sql.DB, error)` | Opens `~/.config/sevens/sevens.db` via the `turso` driver. Enables WAL mode and a 5 s busy timeout. Sets `MaxOpenConns(1)`. |
| `LoadRoots() ([]string, error)` | Reads `~/.config/sevens/roots.edn` and deserializes it as a `[]string`. Returns an empty slice (not an error) if the file does not exist. |
| `SaveRoots(roots []string) error` | Serializes `roots` to EDN and writes `roots.edn`. Overwrites. |
| `AddRoot(root string) error` | Idempotent: loads, appends if not present, saves. |

### Schema (`triples.go`)

| Signature | Description |
|---|---|
| `InitTriplesSchema(db *sql.DB) error` | Creates the `triples` table and three indexes if they do not exist. Idempotent. |

### Subject identity helpers (`triples.go`)

| Signature | Description |
|---|---|
| `NodeSubject(root, title string) string` | Produces the canonical internal subject string for a node. Format: `node:<6-byte-sha1-of-root>:<title>`. |
| `BlockSubject(root, nodeTitle, path string) string` | Produces the canonical internal subject string for a block. Format: `block:<6-byte-sha1-of-root>:<nodeTitle>:<path>`. |
| `NodeTitle(db *sql.DB, subject string) (string, error)` | Resolves a node subject back to its human-readable title by querying the `node/title` predicate. Falls back to returning `subject` itself for legacy rows. |

### Write operations (`triples.go`)

| Signature | Description |
|---|---|
| `InsertTriple(db *sql.DB, t Triple) error` | Inserts one triple. Uses `INSERT OR REPLACE`, so the same `(subject, predicate, object)` triple is idempotent. Does NOT remove other objects for the same `(subject, predicate)` pair. |
| `InsertTriples(db *sql.DB, triples []Triple) error` | Batch insert inside a single transaction using a prepared statement. Same `INSERT OR REPLACE` semantics per row. |
| `SetTriple(db *sql.DB, subject, predicate, object string) error` | Singular-value setter. Deletes all existing rows for `(subject, predicate)` then inserts the new one, in a transaction. Use this for predicates that must have exactly one value. |
| `DeleteBySubject(db *sql.DB, subject string) error` | Removes all triples for a subject. |
| `DeleteBySubjectPrefix(db *sql.DB, prefix string) error` | Removes all triples whose subject begins with `prefix` (SQL `LIKE prefix%`). |
| `DeleteByPredicate(db *sql.DB, predicate string) error` | Removes all triples with a given predicate across all subjects. |
| `ClearRootTriples(db *sql.DB, root string) error` | Finds all subjects that carry `node/root = root` or `block/root = root`, then deletes every triple for those subjects in a transaction. This is the "re-index a root" reset operation. |

### Read / query operations (`triples.go`)

| Signature | Description |
|---|---|
| `GetObject(db *sql.DB, subject, predicate string) (string, error)` | Returns the single object for `(subject, predicate)`, or `""` if none. Use for singular predicates. |
| `GetObjects(db *sql.DB, subject, predicate string) ([]string, error)` | Returns all objects for `(subject, predicate)`. Use for multi-valued predicates. |
| `GetSubjects(db *sql.DB, predicate, object string) ([]string, error)` | Reverse lookup: returns all subjects that have `(predicate, object)`. |
| `GetSubjectTriples(db *sql.DB, subject string) ([]Triple, error)` | Returns all triples for a subject. |
| `GetPredicateTriples(db *sql.DB, predicate string) ([]Triple, error)` | Returns all triples with a given predicate. |
| `ResolveTitle(db *sql.DB, title, root string) string` | Case-insensitive node title lookup within a root. Returns the canonical stored title, or `""` if not found. (Thin wrapper over `ResolveNode`.) |
| `ResolveNode(db *sql.DB, title, root string) (subject, canonical string)` | Case-insensitive lookup. Returns the internal subject and canonical title. Returns empty strings if not found. |
| `SearchContent(db *sql.DB, query, root string) ([]string, error)` | Substring search (`LIKE %query%`) on `node/content` within a root. Returns node titles (not subjects). |
| `SearchTitles(db *sql.DB, query, root string) ([]string, error)` | Substring search on node titles within a root. Returns titles. |
| `ListNodeTitles(db *sql.DB, root string) ([]string, error)` | Returns all node titles for a root. |
| `GetRootNodeData(db *sql.DB, root string, predicates []string) (map[string]map[string][]string, error)` | Batch fetch: returns a map of `title → predicate → []object` for all nodes in a root, for the requested predicates. Avoids N+1 queries. Returns `nil` for empty `predicates`. |
| `RunQuery(db *sql.DB, sqlQuery string, args ...any) ([][]string, error)` | Executes a raw SQL query and returns results as `[][]string`. The first row is always the column header row. Intended for PRQL-compiled queries. |

### Graph traversal (`triples.go`)

| Signature | Description |
|---|---|
| `Compose(db *sql.DB, start, pred1, pred2 string) ([]string, error)` | Two-hop forward traversal: `start →pred1→ intermediate →pred2→ result`. Returns the final objects. |
| `ComposeInverse(db *sql.DB, start, pred1, pred2 string) ([]string, error)` | Mixed traversal: `start →pred1→ intermediate ←pred2← result`. Finds other subjects that share an object with `start` via the two predicates. Excludes `start` itself. Typical use: finding siblings (nodes sharing a parent). |

---

## Unexported Helpers

| Name | Role |
|---|---|
| `scanStrings(rows *sql.Rows) ([]string, error)` | Closes `rows` and scans a single-column string result into a `[]string`. Used by most query functions. |
| `scanTriples(rows *sql.Rows) ([]Triple, error)` | Closes `rows` and scans a `(subject, predicate, object)` result into `[]Triple`. |
| `joinPlaceholders(ph []string) string` | Builds a comma-separated string of placeholders for SQL `IN (...)` clauses. Used by `GetRootNodeData`. |

---

## How the Pieces Relate

### Caller entry point

A typical caller:

1. Calls `OpenDB()` to get a `*sql.DB`.
2. Calls `InitTriplesSchema(db)` once on startup to ensure the table exists.
3. Uses the predicate-namespaced functions (`GetObject`, `GetSubjects`, `InsertTriples`, etc.) to read and write facts.

`LoadRoots` / `SaveRoots` / `AddRoot` are independent of the database; they operate on `roots.edn` in the config directory and are used to track which directories are registered as roots.

### Subject identity

Subjects are opaque strings. `NodeSubject` and `BlockSubject` are the canonical way to produce them. The SHA-1 prefix of the root path scopes subjects so that identically-named nodes in different roots do not collide.

### Predicate conventions

The package enforces no schema at the Go type level. Predicates are strings by convention, and the calling code is responsible for consistency. The comments in the source distinguish between:

- **Singular predicates** (e.g., `node/title`, `node/content`, `node/root`): use `SetTriple` to write, `GetObject` to read.
- **Multi-valued predicates** (e.g., `ref/wiki-link`): use `InsertTriple`/`InsertTriples` to write, `GetObjects` to read.

`ClearRootTriples` specifically knows about `node/root` and `block/root` as the membership predicates that identify which root a subject belongs to.

### Batch vs. per-row access

`GetRootNodeData` exists explicitly to avoid N+1 queries when a caller needs multiple predicates for all nodes in a root. It is the batch alternative to calling `GetObject` in a loop.

### Raw query escape hatch

`RunQuery` provides a generic escape hatch for arbitrary SQL (e.g., output from a PRQL compiler). It always returns column headers as the first row.
