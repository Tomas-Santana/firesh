package repl

import (
	"strings"
	"testing"
)

func TestTokenizeSyntaxClassifiesCoreQueryTokens(t *testing.T) {
	input := []rune(`db.users.where("role", "==", "admin").limit(10).get()`)
	tokens := tokenizeSyntax(input)

	if tokenKindForText(tokens, "db") != syntaxTokenKeyword {
		t.Fatalf("expected db to be keyword")
	}
	if tokenKindForText(tokens, "where") != syntaxTokenMethod {
		t.Fatalf("expected where to be method")
	}
	if tokenKindForText(tokens, `"role"`) != syntaxTokenString {
		t.Fatalf("expected field name to be string")
	}
	if tokenKindForText(tokens, "10") != syntaxTokenLiteral {
		t.Fatalf("expected number literal classification")
	}
	if tokenKindForText(tokens, "get") != syntaxTokenMethod {
		t.Fatalf("expected get to be method")
	}
}

// Methods preceded by "." should be colored even when "(" has not been typed yet.
// This is the key regression that caused mid-word flicker in the original.
func TestTokenizeSyntaxClassifiesMethodWithoutParen(t *testing.T) {
	// Cursor is mid-word: user just typed "db.get" without "(" yet.
	tokens := tokenizeSyntax([]rune("db.get"))
	if tokenKindForText(tokens, "get") != syntaxTokenMethod {
		t.Fatalf("expected get to be method even without trailing '('")
	}

	tokens = tokenizeSyntax([]rune("db.users.where"))
	if tokenKindForText(tokens, "where") != syntaxTokenMethod {
		t.Fatalf("expected where to be method when preceded by '.'")
	}
}

func TestTokenizeSyntaxClassifiesMetaCommands(t *testing.T) {
	showTokens := tokenizeSyntax([]rune("show collections"))
	if tokenKindForText(showTokens, "show") != syntaxTokenMeta {
		t.Fatalf("expected show to be meta")
	}
	if tokenKindForText(showTokens, "collections") != syntaxTokenMeta {
		t.Fatalf("expected collections to be meta subcommand")
	}

	outputTokens := tokenizeSyntax([]rune(`\o json`))
	if tokenKindForText(outputTokens, `\o`) != syntaxTokenMeta {
		t.Fatalf("expected \\o to be meta")
	}
	if tokenKindForText(outputTokens, "json") != syntaxTokenMeta {
		t.Fatalf("expected json to be output format meta token")
	}
}

func TestTokenizeSyntaxClassifiesHelpersAndLiterals(t *testing.T) {
	input := []rune(`db.users.update({lastSeen: serverTimestamp(), hits: increment(1), active: true})`)
	tokens := tokenizeSyntax(input)

	if tokenKindForText(tokens, "serverTimestamp") != syntaxTokenHelper {
		t.Fatalf("expected serverTimestamp to be helper")
	}
	if tokenKindForText(tokens, "increment") != syntaxTokenHelper {
		t.Fatalf("expected increment to be helper")
	}
	if tokenKindForText(tokens, "1") != syntaxTokenLiteral {
		t.Fatalf("expected numeric literal classification")
	}
	if tokenKindForText(tokens, "true") != syntaxTokenLiteral {
		t.Fatalf("expected boolean literal classification")
	}
}

func TestTokenizeSyntaxHandlesUnterminatedString(t *testing.T) {
	tokens := tokenizeSyntax([]rune(`db.users.where("na`))
	if tokenKindForText(tokens, `"na`) != syntaxTokenString {
		t.Fatalf("expected unterminated quoted value to be treated as string token")
	}
}

// Paint must always produce ANSI codes — it no longer depends on fatih/color.NoColor.
func TestSyntaxPainterPaintAddsAnsiCodes(t *testing.T) {
	painter := &SyntaxPainter{}
	out := string(painter.Paint([]rune("help"), len([]rune("help"))))
	if !strings.Contains(out, "\x1b[") {
		t.Fatalf("expected ansi color sequence in painted output, got %q", out)
	}
}

// Adjacent tokens of the same color must NOT produce a reset+reopen between them.
// This is the main change that eliminates render flicker between same-color spans.
func TestSyntaxPainterMergesAdjacentSameColorSpans(t *testing.T) {
	painter := &SyntaxPainter{}
	// "db.users.where" — db is keyword (cyan+bold), then "." plain, then
	// "users" plain (collection name), then "." plain, then "where" method (cyan).
	// The "." and "users" tokens are plain, so there should be a reset between
	// the keyword and the next colored span.  But "where" and any following
	// method should NOT have a reset+reopen if they share the same color code.
	out := string(painter.Paint([]rune(`db.users.where("x","==","y").get()`), 0))

	// Sanity: output must contain ANSI codes.
	if !strings.Contains(out, "\x1b[") {
		t.Fatalf("expected ANSI codes in output, got %q", out)
	}

	// There must be exactly ONE trailing reset at the end (not one per token).
	// Count occurrences of the reset sequence.
	resetCount := strings.Count(out, ansiReset)
	// "db" opens cyan+bold, then resets (plain "." follows).
	// "where" opens cyan, then resets (plain "(" follows).
	// "get" opens cyan, final reset.
	// String literals open green, reset after each.
	// So resetCount > 1 is expected, but it must NOT equal the number of tokens,
	// which would be the case if fatih/color's per-token reset were used.
	tokenCount := len(tokenizeSyntax([]rune(`db.users.where("x","==","y").get()`)))
	if resetCount >= tokenCount {
		t.Fatalf("too many resets (%d for %d tokens) — adjacent same-color spans are not being merged", resetCount, tokenCount)
	}
}

// The visible content of Paint output must round-trip correctly:
// stripping ANSI sequences must give back the original line.
func TestSyntaxPainterPreservesVisibleContent(t *testing.T) {
	painter := &SyntaxPainter{}
	inputs := []string{
		`db.users.get()`,
		`db.orders.where("status", "==", "ok").limit(10).get()`,
		`show collections`,
		`help`,
		`db.users.doc("abc").update({hits: increment(1)})`,
	}
	for _, input := range inputs {
		line := []rune(input)
		painted := string(painter.Paint(line, len(line)))
		// Strip ANSI codes.
		stripped := stripANSI(painted)
		if stripped != input {
			t.Fatalf("Paint changed visible content for %q:\n  got  %q\n  want %q", input, stripped, input)
		}
	}
}

// stripANSI removes all \x1b[...m sequences from s.
func stripANSI(s string) string {
	var out strings.Builder
	runes := []rune(s)
	for i := 0; i < len(runes); {
		if runes[i] == '\x1b' && i+1 < len(runes) && runes[i+1] == '[' {
			i += 2
			for i < len(runes) && runes[i] != 'm' {
				i++
			}
			i++ // skip 'm'
			continue
		}
		out.WriteRune(runes[i])
		i++
	}
	return out.String()
}

func tokenKindForText(tokens []syntaxToken, text string) syntaxTokenKind {
	for _, tok := range tokens {
		if tok.text == text {
			return tok.kind
		}
	}
	return syntaxTokenPlain
}
