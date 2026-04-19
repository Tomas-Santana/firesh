// Package repl implements the interactive read-eval-print loop.
package repl

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"cloud.google.com/go/firestore"
	"github.com/chzyer/readline"
	"github.com/fatih/color"
	"github.com/tomas-santana/firesh/internal/auth"
	"github.com/tomas-santana/firesh/internal/completer"
	"github.com/tomas-santana/firesh/internal/output"
	"github.com/tomas-santana/firesh/internal/query"
)

// ErrExit is returned when the user types exit/quit.
var ErrExit = errors.New("exit")

// REPL holds the interactive shell state.
type REPL struct {
	projectID  string
	databaseID string
	client     *firestore.Client
	printer    *output.Printer
	rl         *readline.Instance
}

// New creates a REPL and connects to Firestore.
func New(projectID, databaseID, outputFmt string) (*REPL, error) {
	ctx := context.Background()
	client, err := auth.NewClient(ctx, projectID, databaseID)
	if err != nil {
		return nil, err
	}

	r := &REPL{
		projectID:  projectID,
		databaseID: databaseID,
		client:     client,
		printer:    output.New(outputFmt),
	}

	rl, err := readline.NewEx(&readline.Config{
		Prompt:          r.prompt(),
		HistoryFile:     "/tmp/firesh_history",
		InterruptPrompt: "^C",
		EOFPrompt:       "exit",
		AutoComplete:    completer.New(),
		Painter:         NewSyntaxPainter(),
	})
	if err != nil {
		return nil, fmt.Errorf("readline: %w", err)
	}
	r.rl = rl
	return r, nil
}

// Run starts the REPL loop.
func (r *REPL) Run() error {
	defer r.rl.Close()
	defer r.client.Close()

	ctx := context.Background()

	color.New(color.Bold, color.FgCyan).Printf("\nfiresh")
	fmt.Printf("  —  project: %s  db: %s\n",
		color.CyanString(r.projectID),
		color.CyanString(r.databaseID))
	fmt.Println("Type 'help' for commands, 'exit' to quit.")
	fmt.Println()

	for {
		line, err := r.rl.Readline()
		if err != nil { // EOF or Ctrl+D
			fmt.Println()
			return nil
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		chain, err := query.Parse(line)
		if err != nil {
			r.printer.PrintError(err)
			continue
		}
		if chain == nil {
			continue
		}

		if execErr := r.executeCommand(ctx, chain); execErr != nil {
			if errors.Is(execErr, ErrExit) {
				return nil
			}
			r.printer.PrintError(execErr)
		}
	}
}

// setFormat switches the output printer format mid-session.
func (r *REPL) setFormat(f string) {
	r.printer = output.New(f)
	color.New(color.FgGreen).Printf("Output format: %s\n", r.printer.Format)
}

// reconnect closes the current client and opens a new one.
func (r *REPL) reconnect(ctx context.Context, projectID, databaseID string) error {
	r.client.Close()
	client, err := auth.NewClient(ctx, projectID, databaseID)
	if err != nil {
		return err
	}
	r.client = client
	r.projectID = projectID
	r.databaseID = databaseID
	r.rl.SetPrompt(r.prompt())
	return nil
}

func (r *REPL) prompt() string {
	db := r.databaseID
	if db == "(default)" {
		db = "default"
	}
	return color.CyanString(fmt.Sprintf("%s/%s> ", r.projectID, db))
}
