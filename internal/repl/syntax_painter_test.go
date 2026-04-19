package repl

import (
	"strings"
	"testing"

	"github.com/fatih/color"
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

func TestSyntaxPainterPaintAddsAnsiCodes(t *testing.T) {
	prevNoColor := color.NoColor
	color.NoColor = false
	defer func() {
		color.NoColor = prevNoColor
	}()

	painter := &SyntaxPainter{}
	out := string(painter.Paint([]rune("help"), len([]rune("help"))))
	if !strings.Contains(out, "\x1b[") {
		t.Fatalf("expected ansi color sequence in painted output, got %q", out)
	}
}

func tokenKindForText(tokens []syntaxToken, text string) syntaxTokenKind {
	for _, tok := range tokens {
		if tok.text == text {
			return tok.kind
		}
	}
	return syntaxTokenPlain
}
