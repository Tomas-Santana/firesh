package repl

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	firestorepb "cloud.google.com/go/firestore/apiv1/firestorepb"
	"github.com/fatih/color"
	"github.com/tomas-santana/firesh/internal/query"
	"google.golang.org/api/iterator"
)

// executeCommand dispatches a parsed Chain to the appropriate handler.
func (r *REPL) executeCommand(ctx context.Context, chain *query.Chain) error {
	switch chain.Terminal {
	case query.TermHelp:
		r.printer.PrintHelp()
	case query.TermClear:
		r.cmdClear()
	case query.TermExit:
		return ErrExit
	case query.TermUse:
		return r.cmdUse(ctx, chain.UseTarget)
	case query.TermShowCollections:
		return r.cmdShowCollections(ctx)
	case query.TermShowDBs:
		r.printer.PrintSuccess(fmt.Sprintf("project: %s  database: %s", r.projectID, r.databaseID))
		fmt.Println("  Manage databases via: gcloud firestore databases list --project=" + r.projectID)
	case query.TermOutputFmt:
		r.setFormat(chain.OutputFmt)
	case query.TermGet:
		return r.cmdGet(ctx, chain)
	case query.TermWatch:
		return r.cmdWatch(ctx, chain)
	case query.TermAdd:
		return r.cmdAdd(ctx, chain)
	case query.TermSet:
		return r.cmdSet(ctx, chain)
	case query.TermUpdate:
		return r.cmdUpdate(ctx, chain)
	case query.TermDelete:
		return r.cmdDelete(ctx, chain)
	case query.TermAggregate:
		return r.cmdAggregate(ctx, chain)
	}
	return nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Meta commands
// ──────────────────────────────────────────────────────────────────────────────

func (r *REPL) cmdUse(ctx context.Context, target string) error {
	project, database := target, "(default)"
	if i := strings.IndexByte(target, '/'); i >= 0 {
		project = target[:i]
		if rest := target[i+1:]; rest != "" {
			database = rest
		}
	}
	if err := r.reconnect(ctx, project, database); err != nil {
		return err
	}
	r.printer.PrintSuccess(fmt.Sprintf("Switched to project: %s  database: %s", project, database))
	return nil
}

func (r *REPL) cmdClear() {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", "cls")
	} else {
		cmd = exec.Command("clear")
	}
	cmd.Stdout = os.Stdout
	cmd.Run()
}

func (r *REPL) cmdShowCollections(ctx context.Context) error {
	iter := r.client.Collections(ctx)
	var names []string
	for {
		col, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return fmt.Errorf("listing collections: %w", err)
		}
		names = append(names, col.ID)
	}
	if len(names) == 0 {
		fmt.Println("(no top-level collections)")
		return nil
	}
	r.printer.PrintList(names)
	return nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Path resolution helpers
// ──────────────────────────────────────────────────────────────────────────────

// resolveRef walks chain.PathSegments to return either a CollectionRef or
// DocumentRef depending on what the terminal path points at.
//
//   - Returns (colRef, nil, nil)  when the last segment is a collection
//   - Returns (nil, docRef, nil)  when the last segment is a doc
func resolveRef(client *firestore.Client, chain *query.Chain) (*firestore.CollectionRef, *firestore.DocumentRef, error) {
	segs := chain.PathSegments
	if len(segs) == 0 {
		return nil, nil, fmt.Errorf("no collection specified")
	}

	var colRef *firestore.CollectionRef
	var docRef *firestore.DocumentRef

	for i, seg := range segs {
		switch seg.Kind {
		case "col":
			if i == 0 {
				colRef = client.Collection(seg.Value)
				docRef = nil
			} else {
				if docRef == nil {
					return nil, nil, fmt.Errorf("collection %q must follow a document", seg.Value)
				}
				colRef = docRef.Collection(seg.Value)
				docRef = nil
			}
		case "doc":
			if colRef == nil {
				return nil, nil, fmt.Errorf("doc() must follow a collection")
			}
			docRef = colRef.Doc(seg.Value)
			colRef = nil
		}
	}
	return colRef, docRef, nil
}

// buildQuery applies filters/ordering/pagination to a base Query.
func buildQuery(q firestore.Query, chain *query.Chain) (firestore.Query, error) {
	// where clauses
	for _, w := range chain.Wheres {
		op, err := normaliseOp(w.Operator)
		if err != nil {
			return q, err
		}
		q = q.Where(w.Field, op, w.Value)
	}

	// whereOr — uses Firestore Or()
	if len(chain.WhereOr) > 0 {
		filters := make([]firestore.EntityFilter, 0, len(chain.WhereOr))
		for _, group := range chain.WhereOr {
			for _, wc := range group {
				op, err := normaliseOp(wc.Operator)
				if err != nil {
					return q, err
				}
				filters = append(filters, firestore.PropertyFilter{
					Path:     wc.Field,
					Operator: op,
					Value:    wc.Value,
				})
			}
		}
		q = q.WhereEntity(firestore.OrFilter{Filters: filters})
	}

	if chain.OrderByField != "" {
		dir := firestore.Asc
		if chain.OrderByDir == "desc" {
			dir = firestore.Desc
		}
		q = q.OrderBy(chain.OrderByField, dir)
	}
	if chain.LimitN > 0 {
		q = q.Limit(chain.LimitN)
	}
	if chain.OffsetN > 0 {
		q = q.Offset(chain.OffsetN)
	}
	if len(chain.SelectedFields) > 0 {
		q = q.Select(chain.SelectedFields...)
	}
	return q, nil
}

// normaliseOp maps user-friendly operator strings to Firestore SDK operators.
func normaliseOp(op string) (string, error) {
	switch op {
	case "==", "!=", ">", ">=", "<", "<=",
		"in", "not-in", "array-contains", "array-contains-any":
		return op, nil
	default:
		return "", fmt.Errorf("unsupported operator %q — valid operators: ==, !=, >, >=, <, <=, in, not-in, array-contains, array-contains-any", op)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// GET
// ──────────────────────────────────────────────────────────────────────────────

func (r *REPL) cmdGet(ctx context.Context, chain *query.Chain) error {
	// Collection group
	if chain.CollectionGroup != "" {
		q, err := buildQuery(r.client.CollectionGroup(chain.CollectionGroup).Query, chain)
		if err != nil {
			return err
		}
		return r.runQuery(ctx, q)
	}

	colRef, docRef, err := resolveRef(r.client, chain)
	if err != nil {
		return err
	}

	// Single document fetch
	if docRef != nil {
		snap, err := docRef.Get(ctx)
		if err != nil {
			return fmt.Errorf("get %s: %w", docRef.Path, err)
		}
		d := snap.Data()
		d["__id__"] = snap.Ref.ID
		r.printer.PrintDocs([]map[string]interface{}{d})
		return nil
	}

	// Collection query
	q, err := buildQuery(colRef.Query, chain)
	if err != nil {
		return err
	}
	return r.runQuery(ctx, q)
}

func (r *REPL) runQuery(ctx context.Context, q firestore.Query) error {
	iter := q.Documents(ctx)
	defer iter.Stop()

	var docs []map[string]interface{}
	for {
		snap, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			// Check for composite index error and surface a helpful link.
			msg := err.Error()
			if strings.Contains(msg, "index") || strings.Contains(msg, "FAILED_PRECONDITION") {
				return fmt.Errorf("%w\n\n  This query requires a composite index.\n  Build it here: https://console.firebase.google.com/project/%s/firestore/indexes", err, r.projectID)
			}
			return err
		}
		d := snap.Data()
		d["__id__"] = snap.Ref.ID
		docs = append(docs, d)
	}
	r.printer.PrintDocs(docs)
	return nil
}

// ──────────────────────────────────────────────────────────────────────────────
// WATCH (real-time listener)
// ──────────────────────────────────────────────────────────────────────────────

func (r *REPL) cmdWatch(ctx context.Context, chain *query.Chain) error {
	color.New(color.FgYellow).Println("Watching for changes — press Ctrl+C to stop")

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Single document watch
	_, docRef, err := resolveRef(r.client, chain)
	if err != nil {
		return err
	}

	if docRef != nil {
		return r.watchDoc(ctx, docRef)
	}

	// Collection / query watch
	colRef, _, err := resolveRef(r.client, chain)
	if err != nil {
		return err
	}
	q, err := buildQuery(colRef.Query, chain)
	if err != nil {
		return err
	}
	return r.watchQuery(ctx, q)
}

func (r *REPL) watchDoc(ctx context.Context, ref *firestore.DocumentRef) error {
	iter := ref.Snapshots(ctx)
	defer iter.Stop()

	for {
		snap, err := iter.Next()
		if err != nil {
			if ctx.Err() != nil {
				return nil // cancelled by user
			}
			return fmt.Errorf("watch: %w", err)
		}
		ts := time.Now().Format("15:04:05")
		color.New(color.FgCyan).Printf("[%s] document update: %s\n", ts, ref.ID)
		d := snap.Data()
		d["__id__"] = snap.Ref.ID
		r.printer.PrintDocs([]map[string]interface{}{d})
	}
}

func (r *REPL) watchQuery(ctx context.Context, q firestore.Query) error {
	iter := q.Snapshots(ctx)
	defer iter.Stop()

	for {
		snap, err := iter.Next()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("watch: %w", err)
		}
		ts := time.Now().Format("15:04:05")
		for _, change := range snap.Changes {
			kind := "UNKNOWN"
			switch change.Kind {
			case firestore.DocumentAdded:
				kind = "ADDED"
			case firestore.DocumentModified:
				kind = "MODIFIED"
			case firestore.DocumentRemoved:
				kind = "REMOVED"
			}
			color.New(color.FgCyan).Printf("[%s] %s %s\n", ts, kind, change.Doc.Ref.ID)
			d := change.Doc.Data()
			d["__id__"] = change.Doc.Ref.ID
			r.printer.PrintDocs([]map[string]interface{}{d})
		}
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// WRITE operations
// ──────────────────────────────────────────────────────────────────────────────

// resolvePayload converts a raw parsed map (which may contain FieldValueSentinels)
// into a map suitable for the Firestore SDK.
func resolvePayload(raw map[string]interface{}) (map[string]interface{}, error) {
	out := make(map[string]interface{}, len(raw))
	for k, v := range raw {
		resolved, err := resolveValue(v)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", k, err)
		}
		out[k] = resolved
	}
	return out, nil
}

func resolveValue(v interface{}) (interface{}, error) {
	switch fv := v.(type) {
	case query.FieldValueSentinel:
		switch fv.Kind {
		case "serverTimestamp":
			return firestore.ServerTimestamp, nil
		case "deleteField":
			return firestore.Delete, nil
		case "increment":
			return firestore.Increment(fv.Delta), nil
		case "arrayUnion":
			return firestore.ArrayUnion(fv.Values...), nil
		case "arrayRemove":
			return firestore.ArrayRemove(fv.Values...), nil
		default:
			return nil, fmt.Errorf("unknown FieldValue kind %q", fv.Kind)
		}
	case map[string]interface{}:
		return resolvePayload(fv)
	default:
		return v, nil
	}
}

// payloadToUpdates converts a flat map to []firestore.Update for merge-style updates.
func payloadToUpdates(doc map[string]interface{}) ([]firestore.Update, error) {
	updates := make([]firestore.Update, 0, len(doc))
	for k, v := range doc {
		val, err := resolveValue(v)
		if err != nil {
			return nil, err
		}
		updates = append(updates, firestore.Update{Path: k, Value: val})
	}
	return updates, nil
}

func (r *REPL) cmdAdd(ctx context.Context, chain *query.Chain) error {
	colRef, _, err := resolveRef(r.client, chain)
	if err != nil {
		return err
	}
	if colRef == nil {
		return fmt.Errorf("add() requires a collection target, not a document ref")
	}
	payload, err := resolvePayload(chain.Doc)
	if err != nil {
		return err
	}
	ref, _, err := colRef.Add(ctx, payload)
	if err != nil {
		return fmt.Errorf("add: %w", err)
	}
	r.printer.PrintSuccess(fmt.Sprintf("Added document — ID: %s", ref.ID))
	return nil
}

func (r *REPL) cmdSet(ctx context.Context, chain *query.Chain) error {
	_, docRef, err := resolveRef(r.client, chain)
	if err != nil {
		return err
	}
	if docRef == nil {
		return fmt.Errorf("set() requires a document target: db.<col>.doc(\"id\").set({...})")
	}
	payload, err := resolvePayload(chain.Doc)
	if err != nil {
		return err
	}
	if _, err := docRef.Set(ctx, payload); err != nil {
		return fmt.Errorf("set %s: %w", docRef.ID, err)
	}
	r.printer.PrintSuccess(fmt.Sprintf("Document set: %s", docRef.ID))
	return nil
}

func (r *REPL) cmdUpdate(ctx context.Context, chain *query.Chain) error {
	colRef, docRef, err := resolveRef(r.client, chain)
	if err != nil {
		return err
	}

	// Single document update
	if docRef != nil {
		updates, err := payloadToUpdates(chain.Doc)
		if err != nil {
			return err
		}
		if _, err := docRef.Update(ctx, updates); err != nil {
			return fmt.Errorf("update %s: %w", docRef.ID, err)
		}
		r.printer.PrintSuccess(fmt.Sprintf("Updated document: %s", docRef.ID))
		return nil
	}

	// Bulk update — query then batch
	if colRef == nil {
		return fmt.Errorf("update requires a collection or document target")
	}
	q, err := buildQuery(colRef.Query, chain)
	if err != nil {
		return err
	}
	updates, err := payloadToUpdates(chain.Doc)
	if err != nil {
		return err
	}
	return r.bulkUpdate(ctx, q, updates)
}

func (r *REPL) bulkUpdate(ctx context.Context, q firestore.Query, updates []firestore.Update) error {
	// First pass: count
	iter := q.Documents(ctx)
	var refs []*firestore.DocumentRef
	for {
		snap, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return err
		}
		refs = append(refs, snap.Ref)
	}
	if len(refs) == 0 {
		r.printer.PrintSuccess("No documents matched — nothing updated")
		return nil
	}
	fmt.Printf("This will update %s document(s). Proceed? (y/n) ", color.YellowString("%d", len(refs)))
	var ans string
	fmt.Scanln(&ans)
	if strings.ToLower(ans) != "y" {
		fmt.Println("Aborted.")
		return nil
	}
	bw := r.client.BulkWriter(ctx)
	for _, ref := range refs {
		if _, err := bw.Update(ref, updates); err != nil {
			return fmt.Errorf("bulk update prepare: %w", err)
		}
	}
	bw.End()
	r.printer.PrintSuccess(fmt.Sprintf("Updated %d document(s)", len(refs)))
	return nil
}

func (r *REPL) cmdDelete(ctx context.Context, chain *query.Chain) error {
	colRef, docRef, err := resolveRef(r.client, chain)
	if err != nil {
		return err
	}

	// Single document delete
	if docRef != nil {
		if _, err := docRef.Delete(ctx); err != nil {
			return fmt.Errorf("delete %s: %w", docRef.ID, err)
		}
		r.printer.PrintSuccess(fmt.Sprintf("Deleted document: %s", docRef.ID))
		return nil
	}

	// Bulk delete via query
	if len(chain.Wheres) == 0 && len(chain.WhereOr) == 0 {
		return fmt.Errorf("bulk delete requires at least one .where() clause to prevent accidental full-collection deletes\nTo delete all documents in a collection, use: db.%s.where(\"__name__\", \">=\", \"\").delete()", colRef.ID)
	}
	q, err := buildQuery(colRef.Query, chain)
	if err != nil {
		return err
	}
	return r.bulkDelete(ctx, q)
}

func (r *REPL) bulkDelete(ctx context.Context, q firestore.Query) error {
	iter := q.Documents(ctx)
	var refs []*firestore.DocumentRef
	for {
		snap, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return err
		}
		refs = append(refs, snap.Ref)
	}
	if len(refs) == 0 {
		r.printer.PrintSuccess("No documents matched — nothing deleted")
		return nil
	}
	fmt.Printf("This will delete %s document(s). Proceed? (y/n) ", color.RedString("%d", len(refs)))
	var ans string
	fmt.Scanln(&ans)
	if strings.ToLower(ans) != "y" {
		fmt.Println("Aborted.")
		return nil
	}
	bw := r.client.BulkWriter(ctx)
	for _, ref := range refs {
		if _, err := bw.Delete(ref); err != nil {
			return fmt.Errorf("bulk delete prepare: %w", err)
		}
	}
	bw.End()
	r.printer.PrintSuccess(fmt.Sprintf("Deleted %d document(s)", len(refs)))
	return nil
}

// ──────────────────────────────────────────────────────────────────────────────
// AGGREGATE
// ──────────────────────────────────────────────────────────────────────────────

func (r *REPL) cmdAggregate(ctx context.Context, chain *query.Chain) error {
	var baseQ firestore.Query
	if chain.CollectionGroup != "" {
		baseQ = r.client.CollectionGroup(chain.CollectionGroup).Query
	} else {
		colRef, docRef, err := resolveRef(r.client, chain)
		if err != nil {
			return err
		}
		if docRef != nil {
			return fmt.Errorf("aggregate() requires a collection target")
		}
		baseQ = colRef.Query
	}

	q, err := buildQuery(baseQ, chain)
	if err != nil {
		return err
	}

	aq := q.NewAggregationQuery()
	for alias, fn := range chain.Aggregations {
		switch fn.Kind {
		case "count":
			aq = aq.WithCount(alias)
		case "sum":
			aq = aq.WithSum(fn.Field, alias)
		case "avg":
			aq = aq.WithAvg(fn.Field, alias)
		default:
			return fmt.Errorf("unknown aggregation %q", fn.Kind)
		}
	}

	results, err := aq.Get(ctx)
	if err != nil {
		return fmt.Errorf("aggregate: %w", err)
	}

	// Print as a simple key-value table
	rows := make([]map[string]interface{}, 0, 1)
	row := map[string]interface{}{}
	for alias, v := range results {
		// SDK returns *firestorepb.Value; unwrap common types
		row[alias] = unwrapAggValue(v)
	}
	rows = append(rows, row)
	r.printer.PrintDocs(rows)
	return nil
}

// unwrapAggValue extracts a Go native value from a *firestorepb.Value.
func unwrapAggValue(v interface{}) interface{} {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case int64:
		return val
	case float64:
		return val
	case *firestorepb.Value:
		switch inner := val.ValueType.(type) {
		case *firestorepb.Value_IntegerValue:
			return inner.IntegerValue
		case *firestorepb.Value_DoubleValue:
			return inner.DoubleValue
		}
		return fmt.Sprintf("%v", val)
	}
	return fmt.Sprintf("%v", v)
}
