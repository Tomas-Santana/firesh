# Changelog

All notable changes to this project will be documented in this file.

## v0.1.0 - 2026-04-19

First public release of firesh.

### Added
- Interactive Firestore REPL with chainable SDK-like syntax.
- Read operations including filtering, ordering, pagination, nested paths, and collection group queries.
- Write operations including add, set, update, delete, and confirmation prompts for bulk operations.
- Aggregate helpers: count, sum, and avg.
- Realtime watch support for collections, documents, and query results.
- Multiple output formats: table, json, and pretty.
- Runtime project and database switching via the use command.
- REPL input syntax highlighting.
- Method completion behavior for chain calls.

### Install
- `go install github.com/tomas-santana/firesh@v0.1.0`
