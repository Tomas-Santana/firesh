// Package output handles pretty-printing of Firestore results.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/fatih/color"
	"github.com/olekukonko/tablewriter"
)

// Format controls how results are rendered.
type Format string

const (
	FormatTable  Format = "table"
	FormatJSON   Format = "json"
	FormatPretty Format = "pretty"
)

// Printer renders documents to stdout.
type Printer struct {
	Format Format
	w      io.Writer
}

// New creates a Printer with the given format string.
func New(format string) *Printer {
	f := Format(strings.ToLower(format))
	switch f {
	case FormatJSON, FormatPretty, FormatTable:
	default:
		f = FormatTable
	}
	return &Printer{Format: f, w: os.Stdout}
}

var (
	bold    = color.New(color.Bold)
	cyan    = color.New(color.FgCyan)
	green   = color.New(color.FgGreen)
	yellow  = color.New(color.FgYellow)
	red     = color.New(color.FgRed)
	magenta = color.New(color.FgMagenta)
)

// PrintDocs renders a slice of documents (each with an injected "__id__" key).
func (p *Printer) PrintDocs(docs []map[string]interface{}) {
	if len(docs) == 0 {
		yellow.Println("(no documents)")
		return
	}
	switch p.Format {
	case FormatJSON:
		p.printJSON(docs)
	case FormatPretty:
		p.printPretty(docs)
	default:
		p.printTable(docs)
	}
	fmt.Printf("\n%s %d document(s)\n", cyan.Sprint("→"), len(docs))
}

// PrintCount prints a count result.
func (p *Printer) PrintCount(n int64) {
	fmt.Printf("%s %s\n", cyan.Sprint("count:"), bold.Sprintf("%d", n))
}

// PrintList renders a simple string list (e.g. collection names).
func (p *Printer) PrintList(items []string) {
	for _, s := range items {
		fmt.Printf("  %s %s\n", cyan.Sprint("•"), s)
	}
}

// PrintSuccess prints a success message.
func (p *Printer) PrintSuccess(msg string) {
	green.Println("✓ " + msg)
}

// PrintError prints an error to stderr.
func (p *Printer) PrintError(err error) {
	red.Fprintf(os.Stderr, "error: %v\n", err)
}

// PrintHelp prints the full command reference.
func (p *Printer) PrintHelp() {
	bold.Println("\n  firesh — Firestore interactive shell")
	fmt.Println()

	section("CONNECTION")
	cmd(`use <project>[/<database>]`, "Switch project (and optionally database) without restarting.")
	cmd(`show collections`, "List top-level collections in the current database.")
	cmd(`show dbs`, "Show current connection info.")
	fmt.Println()

	section("READING")
	cmd(`db.<col>.get()`, "Fetch up to 50 documents.")
	cmd(`db.<col>.doc("id").get()`, "Fetch a single document by ID.")
	cmd(`db.<col>.where("field", "==", value).get()`, "Filter with a where clause.")
	cmd(`db.<col>.where(...).where(...).get()`, "Multiple conditions (implicit AND).")
	cmd(`db.<col>.whereOr(["f","op",v], ["f","op",v]).get()`, "Logical OR conditions.")
	cmd(`db.<col>.orderBy("field", "desc").limit(20).offset(5).get()`, "Sort, paginate.")
	cmd(`db.<col>.select("field1", "field2").get()`, "Select specific fields.")
	cmd(`db.<col>.doc("id").<subCol>.get()`, "Query a nested sub-collection.")
	cmd(`db.collectionGroup("name").where(...).get()`, "Collection group query.")
	fmt.Println()

	section("OPERATORS (for .where())")
	fmt.Println("    ==  !=  >  >=  <  <=  in  not-in  array-contains  array-contains-any")
	fmt.Println()

	section("AGGREGATION")
	cmd(`db.<col>.aggregate({ n: count() })`, "Count all documents.")
	cmd(`db.<col>.where(...).aggregate({ total: sum("amount"), avg: avg("amount") })`, "Sum and average on filtered data.")
	fmt.Println()

	section("WRITING")
	cmd(`db.<col>.add({ field: value })`, "Insert a document (auto-generates ID).")
	cmd(`db.<col>.doc("id").set({ field: value })`, "Set/overwrite a specific document.")
	cmd(`db.<col>.doc("id").update({ field: value })`, "Merge fields into a document.")
	cmd(`db.<col>.where(...).update({ field: value })`, "Bulk update matched documents.")
	cmd(`db.<col>.doc("id").delete()`, "Delete a document by ID.")
	cmd(`db.<col>.where(...).delete()`, "Bulk delete matched documents.")
	fmt.Println()

	section("FIELD VALUE HELPERS  (usable inside any object literal)")
	cmd(`serverTimestamp()`, "Server-side current timestamp.")
	cmd(`increment(n)`, "Atomic numeric increment.")
	cmd(`arrayUnion(v, ...)`, "Add items to an array field.")
	cmd(`arrayRemove(v, ...)`, "Remove items from an array field.")
	cmd(`deleteField()`, "Remove a field from a document.")
	fmt.Println()

	section("REALTIME")
	cmd(`db.<col>.watch()`, "Stream all changes in a collection.")
	cmd(`db.<col>.doc("id").watch()`, "Stream changes to a single document.")
	cmd(`db.<col>.where(...).watch()`, "Stream changes matching a query.")
	fmt.Println("    Press Ctrl+C to stop watching.")
	fmt.Println()

	section("OUTPUT")
	cmd(`\\o table`, "ASCII table (default).")
	cmd(`\\o json`, "Compact JSON array.")
	cmd(`\\o pretty`, "Human-readable indented key: value.")
	fmt.Println()

	section("OTHER")
	cmd(`help | ?`, "Show this help.")
	cmd(`exit | quit | Ctrl+D`, "Exit the shell.")
	fmt.Println()
}

func section(name string) {
	bold.Printf("  %s\n", name)
}

func cmd(syntax, desc string) {
	fmt.Printf("    %-58s %s\n", cyan.Sprint(syntax), desc)
}

// ── renderers ─────────────────────────────────────────────────────────────────

func (p *Printer) printJSON(docs []map[string]interface{}) {
	b, _ := json.Marshal(docs)
	fmt.Println(string(b))
}

func (p *Printer) printPretty(docs []map[string]interface{}) {
	for i, doc := range docs {
		if i > 0 {
			fmt.Println(strings.Repeat("─", 60))
		}
		printPrettyDoc(doc, 0)
	}
}

func printPrettyDoc(m map[string]interface{}, indent int) {
	prefix := strings.Repeat("  ", indent)
	keys := sortedKeys(m)
	for _, k := range keys {
		v := m[k]
		switch val := v.(type) {
		case map[string]interface{}:
			fmt.Printf("%s%s:\n", prefix, magenta.Sprint(k))
			printPrettyDoc(val, indent+1)
		case []interface{}:
			fmt.Printf("%s%s: [", prefix, magenta.Sprint(k))
			for i, item := range val {
				if i > 0 {
					fmt.Print(", ")
				}
				fmt.Print(yellow.Sprint(fmt.Sprintf("%v", item)))
			}
			fmt.Println("]")
		default:
			fmt.Printf("%s%s: %s\n", prefix, magenta.Sprint(k), yellow.Sprint(fmt.Sprintf("%v", val)))
		}
	}
}

func (p *Printer) printTable(docs []map[string]interface{}) {
	colSet := map[string]struct{}{}
	for _, d := range docs {
		for k := range d {
			colSet[k] = struct{}{}
		}
	}
	cols := []string{"__id__"}
	for k := range colSet {
		if k != "__id__" {
			cols = append(cols, k)
		}
	}
	sort.Strings(cols[1:])

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader(cols)
	table.SetAutoWrapText(false)
	table.SetAutoFormatHeaders(true)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetBorder(true)

	for _, doc := range docs {
		row := make([]string, len(cols))
		for i, col := range cols {
			if v, ok := doc[col]; ok {
				row[i] = fmt.Sprintf("%v", v)
			}
		}
		table.Append(row)
	}
	table.Render()
}

func sortedKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
