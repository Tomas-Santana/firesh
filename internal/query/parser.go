// Package query parses a chainable Firestore-native CLI syntax into a Chain struct.
//
// Supported syntax (all methods are chainable, terminal methods trigger execution):
//
// Navigation:
//
//	db.<col>.get()
//	db.<col>.doc("id").<subcol>.get()
//	db.<col>.doc("id").get()
//	db.collectionGroup("name").where(...).get()
//
// Filters:
//
//	.where("field", "==", value)
//	.whereOr(["field","op",value], ["field","op",value])
//
// Sorting / pagination:
//
//	.orderBy("field", "asc"|"desc")
//	.limit(N)
//	.offset(N)
//
// Selections:
//
//	.select("field1", "field2", ...)
//
// Aggregation:
//
//	.aggregate({ total: count(), rev: sum("amount"), avg: avg("amount") })
//
// Mutations:
//
//	db.<col>.add({...})
//	db.<col>.doc("id").set({...})
//	db.<col>.doc("id").update({...})
//	db.<col>.doc("id").delete()
//	db.<col>.where(...).update({...})   ← bulk update
//	db.<col>.where(...).delete()        ← bulk delete
//
// FieldValue helpers (usable inside object literals):
//
//	serverTimestamp()  arrayUnion(v,...)  arrayRemove(v,...)  increment(n)
//
// Realtime:
//
//	db.<col>.watch()
//	db.<col>.doc("id").watch()
//	db.<col>.where(...).watch()
//
// Meta:
//
//	show collections
//	show dbs | show databases
//	use <project>[/<database>]
//	\o table|json|pretty
//	help | ?
//	exit | quit
package query

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// ──────────────────────────────────────────────────────────────────────────────
// Public types
// ──────────────────────────────────────────────────────────────────────────────

// Terminal identifies which action to execute.
type Terminal int

const (
	TermNone Terminal = iota
	TermGet
	TermWatch
	TermAdd
	TermSet
	TermUpdate
	TermDelete
	TermAggregate

	// Meta commands (not Firestore ops)
	TermHelp
	TermExit
	TermShowCollections
	TermShowDBs
	TermUse
	TermOutputFmt
)

// WhereClause holds a single where condition.
type WhereClause struct {
	Field    string
	Operator string
	Value    interface{}
}

// OrGroup holds a set of where conditions ORed together.
type OrGroup []WhereClause

// AggFunc is a requested aggregation function.
type AggFunc struct {
	Kind  string // "count" | "sum" | "avg"
	Field string // empty for count()
}

// Chain is the fully parsed query/command ready for execution.
type Chain struct {
	Terminal Terminal

	// Path segments alternating [collection, docID, collection, docID, ...]
	// The last segment is always a collection (or a docID for doc-terminal ops).
	PathSegments []PathSegment

	// Set when the root is a collection group query.
	CollectionGroup string

	// Filters
	Wheres  []WhereClause
	WhereOr []OrGroup

	// Modifiers
	OrderByField string
	OrderByDir   string // "asc" | "desc"
	LimitN       int    // 0 = unset
	OffsetN      int    // 0 = unset

	// Selections (not implemented yet)
	SelectedFields []string

	// Payloads
	Doc  map[string]interface{} // add / set / update payload
	Docs []map[string]interface{}

	// Aggregations: alias → AggFunc
	Aggregations map[string]AggFunc

	// Meta
	UseTarget string
	OutputFmt string
}

// PathSegment is one hop in the collection/document path.
type PathSegment struct {
	Kind  string // "col" | "doc"
	Value string
}

// ──────────────────────────────────────────────────────────────────────────────
// Entry point
// ──────────────────────────────────────────────────────────────────────────────

// Parse converts a raw input line into a Chain.
// Returns (nil, nil) for blank input.
func Parse(input string) (*Chain, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, nil
	}

	lower := strings.ToLower(input)

	// ── meta commands ──────────────────────────────────────────────────────────
	switch {
	case lower == "help" || lower == "?":
		return &Chain{Terminal: TermHelp}, nil
	case lower == "exit" || lower == "quit":
		return &Chain{Terminal: TermExit}, nil
	case lower == "show collections":
		return &Chain{Terminal: TermShowCollections}, nil
	case lower == "show dbs" || lower == "show databases":
		return &Chain{Terminal: TermShowDBs}, nil
	case strings.HasPrefix(lower, "use "):
		target := strings.TrimSpace(input[4:])
		if target == "" {
			return nil, fmt.Errorf("usage: use <projectID>[/<databaseID>]")
		}
		return &Chain{Terminal: TermUse, UseTarget: target}, nil
	case strings.HasPrefix(input, `\o `):
		f := strings.TrimSpace(input[3:])
		return &Chain{Terminal: TermOutputFmt, OutputFmt: f}, nil
	}

	// ── must start with "db." ──────────────────────────────────────────────────
	if !strings.HasPrefix(input, "db.") {
		return nil, fmt.Errorf("unknown command: %q\nType 'help' for available commands", input)
	}

	p := &parser{src: input[3:]} // strip leading "db."
	return p.parseChain()
}

// ──────────────────────────────────────────────────────────────────────────────
// Parser
// ──────────────────────────────────────────────────────────────────────────────

type parser struct {
	src string
	pos int
}

func (p *parser) parseChain() (*Chain, error) {
	chain := &Chain{LimitN: 50} // sensible default

	// First token: either collectionGroup("name") or an identifier (collection name)
	if strings.HasPrefix(p.remaining(), "collectionGroup(") {
		p.advance(len("collectionGroup("))
		name, err := p.parseStringArg()
		if err != nil {
			return nil, fmt.Errorf("collectionGroup: %w", err)
		}
		if err := p.expect(')'); err != nil {
			return nil, err
		}
		chain.CollectionGroup = name
	} else {
		col, err := p.parseIdent()
		if err != nil {
			return nil, fmt.Errorf("expected collection name after 'db.': %w", err)
		}
		chain.PathSegments = append(chain.PathSegments, PathSegment{Kind: "col", Value: col})
	}

	// Parse chain methods until end of input
	for p.pos < len(p.src) {
		if p.src[p.pos] != '.' {
			return nil, fmt.Errorf("unexpected character %q at position %d", p.src[p.pos], p.pos)
		}
		p.pos++ // consume '.'

		method, err := p.parseIdent()
		if err != nil {
			return nil, fmt.Errorf("expected method name: %w", err)
		}

		switch strings.ToLower(method) {

		// ── navigation ──────────────────────────────────────────────────────
		case "doc":
			if err := p.expect('('); err != nil {
				return nil, err
			}
			id, err := p.parseStringArg()
			if err != nil {
				return nil, fmt.Errorf("doc(): %w", err)
			}
			if err := p.expect(')'); err != nil {
				return nil, err
			}
			chain.PathSegments = append(chain.PathSegments, PathSegment{Kind: "doc", Value: id})

			// Next might be a sub-collection identifier (not a method call)
			if p.pos < len(p.src) && p.src[p.pos] == '.' {
				// peek at next token to see if it's a plain identifier (collection)
				// rather than a known method — defer to outer loop
			}

		// ── filters ─────────────────────────────────────────────────────────
		case "where":
			wc, err := p.parseWhereArgs()
			if err != nil {
				return nil, fmt.Errorf("where(): %w", err)
			}
			chain.Wheres = append(chain.Wheres, wc)

		case "whereor":
			groups, err := p.parseWhereOrArgs()
			if err != nil {
				return nil, fmt.Errorf("whereOr(): %w", err)
			}
			chain.WhereOr = append(chain.WhereOr, groups...)

		// ── modifiers ────────────────────────────────────────────────────────
		case "orderby":
			field, dir, err := p.parseOrderByArgs()
			if err != nil {
				return nil, fmt.Errorf("orderBy(): %w", err)
			}
			chain.OrderByField = field
			chain.OrderByDir = dir

		case "limit":
			n, err := p.parseIntArg()
			if err != nil {
				return nil, fmt.Errorf("limit(): %w", err)
			}
			chain.LimitN = n

		case "offset":
			n, err := p.parseIntArg()
			if err != nil {
				return nil, fmt.Errorf("offset(): %w", err)
			}
			chain.OffsetN = n

		case "select":
			fields, err := p.parseSelectArgs()
			if err != nil {
				return nil, fmt.Errorf("select(): %w", err)
			}
			chain.SelectedFields = fields

		// ── terminals ────────────────────────────────────────────────────────
		case "get":
			if err := p.expectEmptyParens(); err != nil {
				return nil, fmt.Errorf("get(): %w", err)
			}
			chain.Terminal = TermGet
			return chain, nil

		case "watch":
			if err := p.expectEmptyParens(); err != nil {
				return nil, fmt.Errorf("watch(): %w", err)
			}
			chain.Terminal = TermWatch
			return chain, nil

		case "add":
			doc, err := p.parseObjectArg()
			if err != nil {
				return nil, fmt.Errorf("add(): %w", err)
			}
			chain.Doc = doc
			chain.Terminal = TermAdd
			return chain, nil

		case "set":
			doc, err := p.parseObjectArg()
			if err != nil {
				return nil, fmt.Errorf("set(): %w", err)
			}
			chain.Doc = doc
			chain.Terminal = TermSet
			return chain, nil

		case "update":
			doc, err := p.parseObjectArg()
			if err != nil {
				return nil, fmt.Errorf("update(): %w", err)
			}
			chain.Doc = doc
			chain.Terminal = TermUpdate
			return chain, nil

		case "delete":
			if err := p.expectEmptyParens(); err != nil {
				return nil, fmt.Errorf("delete(): %w", err)
			}
			chain.Terminal = TermDelete
			return chain, nil

		case "aggregate":
			aggs, err := p.parseAggregateArg()
			if err != nil {
				return nil, fmt.Errorf("aggregate(): %w", err)
			}
			chain.Aggregations = aggs
			chain.Terminal = TermAggregate
			return chain, nil

		default:
			// Could be a sub-collection name (after .doc("id").subCol.get())
			// If it looks like a plain identifier and there's more chain, treat as collection
			if isIdent(method) {
				chain.PathSegments = append(chain.PathSegments, PathSegment{Kind: "col", Value: method})
			} else {
				return nil, fmt.Errorf("unknown method: %q\nType 'help' for available commands", method)
			}
		}
	}

	return nil, fmt.Errorf("incomplete command — missing terminal method (.get(), .watch(), .add(), .set(), .update(), .delete(), .aggregate())")
}

// ──────────────────────────────────────────────────────────────────────────────
// Argument parsers
// ──────────────────────────────────────────────────────────────────────────────

// parseWhereArgs parses ("field", "op", value)
func (p *parser) parseWhereArgs() (WhereClause, error) {
	if err := p.expect('('); err != nil {
		return WhereClause{}, err
	}
	field, err := p.parseStringArg()
	if err != nil {
		return WhereClause{}, fmt.Errorf("field: %w", err)
	}
	if err := p.expectComma(); err != nil {
		return WhereClause{}, err
	}
	op, err := p.parseStringArg()
	if err != nil {
		return WhereClause{}, fmt.Errorf("operator: %w", err)
	}
	if err := p.expectComma(); err != nil {
		return WhereClause{}, err
	}
	val, err := p.parseValue()
	if err != nil {
		return WhereClause{}, fmt.Errorf("value: %w", err)
	}
	p.skipSpace()
	if err := p.expect(')'); err != nil {
		return WhereClause{}, err
	}
	return WhereClause{Field: field, Operator: op, Value: val}, nil
}

// parseWhereOrArgs parses (["f","op",v], ["f","op",v], ...)
func (p *parser) parseWhereOrArgs() ([]OrGroup, error) {
	if err := p.expect('('); err != nil {
		return nil, err
	}
	var groups []OrGroup
	for {
		p.skipSpace()
		if p.pos >= len(p.src) {
			return nil, fmt.Errorf("unterminated whereOr()")
		}
		if p.src[p.pos] == ')' {
			p.pos++
			break
		}
		if p.src[p.pos] == '[' {
			p.pos++
			field, err := p.parseStringArg()
			if err != nil {
				return nil, fmt.Errorf("whereOr field: %w", err)
			}
			if err := p.expectComma(); err != nil {
				return nil, err
			}
			op, err := p.parseStringArg()
			if err != nil {
				return nil, fmt.Errorf("whereOr operator: %w", err)
			}
			if err := p.expectComma(); err != nil {
				return nil, err
			}
			val, err := p.parseValue()
			if err != nil {
				return nil, fmt.Errorf("whereOr value: %w", err)
			}
			p.skipSpace()
			if err := p.expect(']'); err != nil {
				return nil, err
			}
			groups = append(groups, OrGroup{WhereClause{Field: field, Operator: op, Value: val}})
		}
		p.skipSpace()
		if p.pos < len(p.src) && p.src[p.pos] == ',' {
			p.pos++
		}
	}
	return groups, nil
}

func (p *parser) parseOrderByArgs() (string, string, error) {
	if err := p.expect('('); err != nil {
		return "", "", err
	}
	field, err := p.parseStringArg()
	if err != nil {
		return "", "", fmt.Errorf("field: %w", err)
	}
	dir := "asc"
	p.skipSpace()
	if p.pos < len(p.src) && p.src[p.pos] == ',' {
		p.pos++
		d, err := p.parseStringArg()
		if err != nil {
			return "", "", fmt.Errorf("direction: %w", err)
		}
		dir = strings.ToLower(d)
		if dir != "asc" && dir != "desc" {
			return "", "", fmt.Errorf("direction must be \"asc\" or \"desc\", got %q", dir)
		}
	}
	p.skipSpace()
	if err := p.expect(')'); err != nil {
		return "", "", err
	}
	return field, dir, nil
}

func (p *parser) parseIntArg() (int, error) {
	if err := p.expect('('); err != nil {
		return 0, err
	}
	p.skipSpace()
	start := p.pos
	for p.pos < len(p.src) && (p.src[p.pos] >= '0' && p.src[p.pos] <= '9') {
		p.pos++
	}
	if p.pos == start {
		return 0, fmt.Errorf("expected integer")
	}
	n, _ := strconv.Atoi(p.src[start:p.pos])
	p.skipSpace()
	if err := p.expect(')'); err != nil {
		return 0, err
	}
	return n, nil
}

func (p *parser) parseSelectArgs() ([]string, error) {
	if err := p.expect('('); err != nil {
		return nil, err
	}
	var fields []string
	for {
		field, err := p.parseStringArg()
		if err != nil {
			return nil, fmt.Errorf("select field: %w", err)
		}
		fields = append(fields, field)
		p.skipSpace()
		if p.pos < len(p.src) && p.src[p.pos] == ',' {
			p.pos++
		} else {
			break
		}
	}
	p.skipSpace()
	if err := p.expect(')'); err != nil {
		return nil, err
	}
	return fields, nil
}

func (p *parser) parseObjectArg() (map[string]interface{}, error) {
	if err := p.expect('('); err != nil {
		return nil, err
	}
	p.skipSpace()
	obj, err := p.parseObjectLiteral()
	if err != nil {
		return nil, err
	}
	p.skipSpace()
	if err := p.expect(')'); err != nil {
		return nil, err
	}
	return obj, nil
}

// parseAggregateArg parses ({ alias: count(), alias2: sum("field") })
func (p *parser) parseAggregateArg() (map[string]AggFunc, error) {
	if err := p.expect('('); err != nil {
		return nil, err
	}
	p.skipSpace()
	if err := p.expect('{'); err != nil {
		return nil, err
	}

	result := map[string]AggFunc{}
	for {
		p.skipSpace()
		if p.pos >= len(p.src) {
			return nil, fmt.Errorf("unterminated aggregate object")
		}
		if p.src[p.pos] == '}' {
			p.pos++
			break
		}
		// parse alias
		alias, err := p.parseIdent()
		if err != nil {
			return nil, fmt.Errorf("aggregate alias: %w", err)
		}
		p.skipSpace()
		if err := p.expect(':'); err != nil {
			return nil, err
		}
		p.skipSpace()
		fn, err := p.parseAggFn()
		if err != nil {
			return nil, fmt.Errorf("aggregate function for %q: %w", alias, err)
		}
		result[alias] = fn
		p.skipSpace()
		if p.pos < len(p.src) && p.src[p.pos] == ',' {
			p.pos++
		}
	}
	p.skipSpace()
	if err := p.expect(')'); err != nil {
		return nil, err
	}
	return result, nil
}

func (p *parser) parseAggFn() (AggFunc, error) {
	name, err := p.parseIdent()
	if err != nil {
		return AggFunc{}, err
	}
	name = strings.ToLower(name)
	switch name {
	case "count":
		if err := p.expectEmptyParens(); err != nil {
			return AggFunc{}, err
		}
		return AggFunc{Kind: "count"}, nil
	case "sum", "avg":
		if err := p.expect('('); err != nil {
			return AggFunc{}, err
		}
		field, err := p.parseStringArg()
		if err != nil {
			return AggFunc{}, fmt.Errorf("%s field: %w", name, err)
		}
		if err := p.expect(')'); err != nil {
			return AggFunc{}, err
		}
		return AggFunc{Kind: name, Field: field}, nil
	default:
		return AggFunc{}, fmt.Errorf("unknown aggregation function %q (use count(), sum(\"field\"), avg(\"field\"))", name)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Value parser — JSON values + FieldValue helpers
// ──────────────────────────────────────────────────────────────────────────────

// FieldValueSentinel marks special Firestore FieldValue operations.
type FieldValueSentinel struct {
	Kind   string        // "serverTimestamp" | "arrayUnion" | "arrayRemove" | "increment" | "deleteField"
	Values []interface{} // for arrayUnion/arrayRemove
	Delta  float64       // for increment
}

// parseValue reads a single JSON value (string, number, bool, null, array, object)
// OR a FieldValue helper call.
func (p *parser) parseValue() (interface{}, error) {
	p.skipSpace()
	if p.pos >= len(p.src) {
		return nil, fmt.Errorf("unexpected end of input")
	}

	// Check for FieldValue helpers or identifier literals (true/false/null)
	if isIdentStart(p.src[p.pos]) {
		return p.parseIdentOrHelper()
	}

	// Delegate rest to JSON tokeniser
	return p.parseJSONValue()
}

func (p *parser) parseIdentOrHelper() (interface{}, error) {
	ident, err := p.parseIdent()
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(ident) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "null":
		return nil, nil
	case "servertimestamp":
		if err := p.expectEmptyParens(); err != nil {
			return nil, err
		}
		return FieldValueSentinel{Kind: "serverTimestamp"}, nil
	case "deletefield":
		if err := p.expectEmptyParens(); err != nil {
			return nil, err
		}
		return FieldValueSentinel{Kind: "deleteField"}, nil
	case "arrayunion":
		vals, err := p.parseValueList()
		if err != nil {
			return nil, fmt.Errorf("arrayUnion: %w", err)
		}
		return FieldValueSentinel{Kind: "arrayUnion", Values: vals}, nil
	case "arrayremove":
		vals, err := p.parseValueList()
		if err != nil {
			return nil, fmt.Errorf("arrayRemove: %w", err)
		}
		return FieldValueSentinel{Kind: "arrayRemove", Values: vals}, nil
	case "increment":
		if err := p.expect('('); err != nil {
			return nil, err
		}
		val, err := p.parseJSONValue()
		if err != nil {
			return nil, fmt.Errorf("increment: %w", err)
		}
		p.skipSpace()
		if err := p.expect(')'); err != nil {
			return nil, err
		}
		delta, ok := val.(float64)
		if !ok {
			return nil, fmt.Errorf("increment: expected a number")
		}
		return FieldValueSentinel{Kind: "increment", Delta: delta}, nil
	default:
		return nil, fmt.Errorf("unknown identifier %q — did you mean a string? Use quotes: %q", ident, ident)
	}
}

func (p *parser) parseValueList() ([]interface{}, error) {
	if err := p.expect('('); err != nil {
		return nil, err
	}
	var vals []interface{}
	for {
		p.skipSpace()
		if p.pos >= len(p.src) {
			return nil, fmt.Errorf("unterminated argument list")
		}
		if p.src[p.pos] == ')' {
			p.pos++
			break
		}
		v, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		vals = append(vals, v)
		p.skipSpace()
		if p.pos < len(p.src) && p.src[p.pos] == ',' {
			p.pos++
		}
	}
	return vals, nil
}

// parseJSONValue reads a standard JSON value from the current position.
func (p *parser) parseJSONValue() (interface{}, error) {
	p.skipSpace()
	if p.pos >= len(p.src) {
		return nil, fmt.Errorf("unexpected end of input")
	}
	ch := p.src[p.pos]
	switch {
	case ch == '"' || ch == '\'':
		return p.parseQuotedString()
	case ch == '{':
		return p.parseObjectLiteral()
	case ch == '[':
		return p.parseArrayLiteral()
	case ch == '-' || (ch >= '0' && ch <= '9'):
		return p.parseNumber()
	default:
		return nil, fmt.Errorf("unexpected character %q", ch)
	}
}

func (p *parser) parseQuotedString() (string, error) {
	if p.pos >= len(p.src) {
		return "", fmt.Errorf("expected string")
	}
	quote := p.src[p.pos]
	if quote != '"' && quote != '\'' {
		return "", fmt.Errorf("expected quoted string, got %q", quote)
	}
	p.pos++
	var sb strings.Builder
	for p.pos < len(p.src) {
		ch := p.src[p.pos]
		if ch == '\\' && p.pos+1 < len(p.src) {
			p.pos++
			switch p.src[p.pos] {
			case 'n':
				sb.WriteByte('\n')
			case 't':
				sb.WriteByte('\t')
			case '\\':
				sb.WriteByte('\\')
			default:
				sb.WriteByte(p.src[p.pos])
			}
			p.pos++
			continue
		}
		if ch == quote {
			p.pos++
			return sb.String(), nil
		}
		sb.WriteByte(ch)
		p.pos++
	}
	return "", fmt.Errorf("unterminated string")
}

func (p *parser) parseObjectLiteral() (map[string]interface{}, error) {
	if err := p.expect('{'); err != nil {
		return nil, err
	}
	m := map[string]interface{}{}
	for {
		p.skipSpace()
		if p.pos >= len(p.src) {
			return nil, fmt.Errorf("unterminated object")
		}
		if p.src[p.pos] == '}' {
			p.pos++
			return m, nil
		}
		// key — string or bare identifier
		var key string
		var err error
		if p.src[p.pos] == '"' || p.src[p.pos] == '\'' {
			key, err = p.parseQuotedString()
		} else {
			key, err = p.parseIdent()
		}
		if err != nil {
			return nil, fmt.Errorf("object key: %w", err)
		}
		p.skipSpace()
		if err := p.expect(':'); err != nil {
			return nil, err
		}
		p.skipSpace()
		val, err := p.parseValue()
		if err != nil {
			return nil, fmt.Errorf("value for key %q: %w", key, err)
		}
		m[key] = val
		p.skipSpace()
		if p.pos < len(p.src) && p.src[p.pos] == ',' {
			p.pos++
		}
	}
}

func (p *parser) parseArrayLiteral() ([]interface{}, error) {
	if err := p.expect('['); err != nil {
		return nil, err
	}
	var arr []interface{}
	for {
		p.skipSpace()
		if p.pos >= len(p.src) {
			return nil, fmt.Errorf("unterminated array")
		}
		if p.src[p.pos] == ']' {
			p.pos++
			return arr, nil
		}
		v, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		arr = append(arr, v)
		p.skipSpace()
		if p.pos < len(p.src) && p.src[p.pos] == ',' {
			p.pos++
		}
	}
}

func (p *parser) parseNumber() (interface{}, error) {
	start := p.pos
	if p.pos < len(p.src) && p.src[p.pos] == '-' {
		p.pos++
	}
	for p.pos < len(p.src) && (p.src[p.pos] >= '0' && p.src[p.pos] <= '9') {
		p.pos++
	}
	if p.pos < len(p.src) && p.src[p.pos] == '.' {
		p.pos++
		for p.pos < len(p.src) && (p.src[p.pos] >= '0' && p.src[p.pos] <= '9') {
			p.pos++
		}
	}
	raw := p.src[start:p.pos]
	var v interface{}
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return nil, fmt.Errorf("invalid number %q", raw)
	}
	return v, nil
}

// parseStringArg reads the next quoted string (used for named args like field names).
func (p *parser) parseStringArg() (string, error) {
	p.skipSpace()
	return p.parseQuotedString()
}

// ──────────────────────────────────────────────────────────────────────────────
// Low-level helpers
// ──────────────────────────────────────────────────────────────────────────────

func (p *parser) parseIdent() (string, error) {
	p.skipSpace()
	if p.pos >= len(p.src) || !isIdentStart(p.src[p.pos]) {
		return "", fmt.Errorf("expected identifier at %q", p.remaining())
	}
	start := p.pos
	for p.pos < len(p.src) && isIdentChar(p.src[p.pos]) {
		p.pos++
	}
	return p.src[start:p.pos], nil
}

func (p *parser) expect(ch byte) error {
	p.skipSpace()
	if p.pos >= len(p.src) {
		return fmt.Errorf("expected %q but reached end of input", ch)
	}
	if p.src[p.pos] != ch {
		return fmt.Errorf("expected %q, got %q at position %d", ch, p.src[p.pos], p.pos)
	}
	p.pos++
	return nil
}

func (p *parser) expectComma() error {
	p.skipSpace()
	if p.pos >= len(p.src) || p.src[p.pos] != ',' {
		return fmt.Errorf("expected ','")
	}
	p.pos++
	return nil
}

func (p *parser) expectEmptyParens() error {
	if err := p.expect('('); err != nil {
		return err
	}
	p.skipSpace()
	return p.expect(')')
}

func (p *parser) skipSpace() {
	for p.pos < len(p.src) && unicode.IsSpace(rune(p.src[p.pos])) {
		p.pos++
	}
}

func (p *parser) remaining() string {
	if p.pos >= len(p.src) {
		return ""
	}
	return p.src[p.pos:]
}

func (p *parser) advance(n int) {
	p.pos += n
}

func isIdentStart(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_'
}

// IsIdentStart reports whether c can start an unquoted identifier.
func IsIdentStart(c byte) bool {
	return isIdentStart(c)
}

func isIdentChar(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
}

// IsIdentChar reports whether c can appear in an unquoted identifier.
func IsIdentChar(c byte) bool {
	return isIdentChar(c)
}

func isIdent(s string) bool {
	if len(s) == 0 {
		return false
	}
	if !isIdentStart(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		if !isIdentChar(s[i]) {
			return false
		}
	}
	return true
}
