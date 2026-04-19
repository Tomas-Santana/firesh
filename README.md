# Firesh

A Firestore-native interactive CLI shell for Google Cloud Firestore, written in Go.

Commands mirror the official Firebase SDK — chainable `.where()`, `.orderBy()`, `.limit()`, and terminal methods like `.get()`, `.watch()`, `.update()`, `.delete()`. There is no translation layer; the syntax maps 1:1 to Firestore's query model.

```
firesh  —  project: my-project  db: default
Type 'help' for commands, 'exit' to quit.

my-project/default> db.orders
  .where("status", "==", "completed")
  .where("total", ">=", 100)
  .orderBy("createdAt", "desc")
  .limit(20)
  .get()
```

---

## Installation

```bash
go install github.com/tomas-santana/firesh@latest

# Install this release explicitly
go install github.com/tomas-santana/firesh@v0.1.0
```

For older Go workflows that still support version-less install, the module path is:

```bash
go install github.com/tomas-santana/firesh
```

## Authentication

firesh uses **Google Application Default Credentials (ADC)** — no config files or API keys required.

**Option 1 — gcloud (recommended for local dev):**
```bash
gcloud auth application-default login
```

**Option 2 — Service account key file:**
```bash
export GOOGLE_APPLICATION_CREDENTIALS=/path/to/key.json
```

**Option 3 — Attached service account** (GCE / Cloud Run / GKE — works automatically).

---

## Usage

```bash
firesh --project my-gcp-project
firesh --project my-gcp-project --database my-db
firesh --project my-gcp-project --output json

# Or use environment variable
export GOOGLE_CLOUD_PROJECT=my-gcp-project
firesh
```

### Flags

| Flag | Short | Default | Description |
|---|---|---|---|
| `--project` | `-p` | `$GOOGLE_CLOUD_PROJECT` | GCP project ID |
| `--database` | `-d` | `(default)` | Firestore database ID |
| `--output` | `-o` | `table` | Output format: `table`, `json`, `pretty` |

---

## Command Reference

### Connection & Navigation

```
use other-project
use other-project/analytics-db

show collections
show dbs
```

### Reading

```js
// Fetch up to 50 documents (default)
db.users.get()

// Fetch a single document by ID
db.users.doc("alice_123").get()

// Single where clause
db.users.where("role", "==", "admin").get()

// Multiple conditions — implicit AND
db.orders
  .where("status", "==", "completed")
  .where("total", ">=", 100)
  .where("tags", "array-contains", "express")
  .get()

// Logical OR
db.users.whereOr(
  ["status", "==", "banned"],
  ["lastLogin", "<", "2023-01-01"]
).get()

// Sorting and pagination
db.users
  .where("status", "==", "active")
  .orderBy("createdAt", "desc")
  .limit(20)
  .offset(40)
  .get()

// Nested sub-collection
db.users.doc("alice_123").posts.get()
db.users.doc("alice_123").posts.doc("post_456").comments.where("flagged", "==", true).get()

// Collection group (all collections with this name, at any depth)
db.collectionGroup("comments").where("status", "==", "flagged").get()
```

### Supported Operators

`==`  `!=`  `>`  `>=`  `<`  `<=`  `in`  `not-in`  `array-contains`  `array-contains-any`

> If a compound query requires a composite index, firesh prints an error with a direct link to build it in the Firebase Console.

### Aggregation

```js
// Count all documents
db.transactions.aggregate({ total: count() })

// Sum and average on filtered data
db.transactions
  .where("status", "==", "successful")
  .where("date", ">=", "2024-01-01")
  .aggregate({
    revenue: sum("amount"),
    avgValue: avg("amount")
  })
```

### Writing

```js
// Add a document (Firestore auto-generates the ID)
db.users.add({ name: "Alice", role: "admin", active: true })

// Set a specific document ID (overwrites entirely)
db.users.doc("alice_123").set({ name: "Alice", role: "admin" })

// Merge fields into an existing document
db.users.doc("alice_123").update({ role: "superadmin" })

// Bulk update — matches query, prompts for confirmation
db.users
  .where("lastLogin", "<", "2023-01-01")
  .update({ status: "archived" })
// → "This will update 420 documents. Proceed? (y/n)"

// Delete a single document
db.users.doc("alice_123").delete()

// Bulk delete — prompts for confirmation
db.users.where("status", "==", "banned").delete()
// → "This will delete 12 documents. Proceed? (y/n)"
```

### FieldValue Helpers

Use these inside any object literal for atomic server-side operations:

```js
db.users.doc("alice_123").update({
  lastUpdated: serverTimestamp(),   // server-side timestamp
  loginCount:  increment(1),        // atomic increment
  tags:        arrayUnion("beta"),  // add to array
  oldTag:      arrayRemove("alpha"),// remove from array
  tempField:   deleteField()        // remove the field entirely
})
```

### Real-time Watching

Streams live Firestore changes to the terminal. Press **Ctrl+C** to stop.

```js
// Watch all documents in a collection
db.users.watch()

// Watch a single document
db.users.doc("alice_123").watch()

// Watch a filtered query
db.orders.where("status", "==", "pending").watch()

// Watch a nested sub-collection
db.users.doc("alice_123").posts.watch()
```

Each change is printed with a timestamp and change type (`ADDED`, `MODIFIED`, `REMOVED`).

### Output Format

Switch format mid-session with `\o`:

```
\o table     # ASCII table (default)
\o json      # Compact JSON array
\o pretty    # Indented key: value per document
```

Or set it at startup with `--output json`.

---

## Notes

- **Bulk operations** (`.where(...).update()` / `.where(...).delete()`) always prompt for confirmation before executing.
- **Bulk deletes** require at least one `.where()` clause to prevent accidental full-collection drops.
- **Composite index errors** include a direct Firebase Console link to create the required index.
- **Shell history** is persisted to `/tmp/firesh_history`.
- The `use` command reconnects the Firestore client live — no restart needed.