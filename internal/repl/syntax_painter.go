package repl

import (
	"strings"
	"unicode"

	"github.com/chzyer/readline"
	"github.com/tomas-santana/firesh/internal/query"
)

// ──────────────────────────────────────────────────────────────────────────────
// Token kinds
// ──────────────────────────────────────────────────────────────────────────────

type syntaxTokenKind int

const (
	syntaxTokenPlain syntaxTokenKind = iota
	syntaxTokenKeyword
	syntaxTokenMethod
	syntaxTokenMeta
	syntaxTokenHelper
	syntaxTokenString
	syntaxTokenLiteral
)

type syntaxToken struct {
	kind syntaxTokenKind
	text string
}

// ──────────────────────────────────────────────────────────────────────────────
// Token vocabulary
// ──────────────────────────────────────────────────────────────────────────────

var syntaxKeywordWords = map[string]struct{}{
	"db": {},
}

var syntaxMethodWords = map[string]struct{}{
	"collectiongroup": {},
	"doc":             {},
	"where":           {},
	"whereor":         {},
	"orderby":         {},
	"limit":           {},
	"offset":          {},
	"select":          {},
	"get":             {},
	"watch":           {},
	"add":             {},
	"set":             {},
	"update":          {},
	"delete":          {},
	"aggregate":       {},
}

var syntaxMetaWords = map[string]struct{}{
	"help": {},
	"exit": {},
	"quit": {},
	"show": {},
	"use":  {},
	"?":    {},
	`\o`:   {},
}

var syntaxShowSubcommandWords = map[string]struct{}{
	"collections": {},
	"dbs":         {},
	"databases":   {},
}

var syntaxOutputFormatWords = map[string]struct{}{
	"table":  {},
	"json":   {},
	"pretty": {},
}

var syntaxHelperWords = map[string]struct{}{
	"servertimestamp": {},
	"deletefield":     {},
	"arrayunion":      {},
	"arrayremove":     {},
	"increment":       {},
	"count":           {},
	"sum":             {},
	"avg":             {},
}

var syntaxLiteralWords = map[string]struct{}{
	"true":  {},
	"false": {},
	"null":  {},
}

// ──────────────────────────────────────────────────────────────────────────────
// Minimal ANSI helpers
//
// We intentionally avoid fatih/color's Sprint functions here.  Those functions
// return a Go string whose ANSI bytes get converted to individual []rune
// elements — each ASCII byte inside the escape sequence (e.g. '[', '3', '6',
// 'm') has rune width 1 in readline's runes.Width().  The mismatch between the
// number of runes written by Paint and the number of backspaces emitted by
// readline's getBackspaceSequence() (which uses the original r.buf length)
// means the cursor drifts, producing the blinking / disappearing characters.
//
// By writing raw ANSI escapes as a plain string prefix/suffix around the
// visible text — and keeping the returned []rune length equal to the original
// line length — we avoid that drift entirely.
//
// Concretely: instead of returning ansi("db") as a []rune of 16 elements, we
// embed the escape sequences into the string but convert the whole painted
// output to []rune only once at the end.  What matters is that the TOTAL byte
// output is correct for the terminal (it is — terminals don't advance the
// cursor for CSI sequences) and that readline's cursor math uses r.buf, not
// the Paint output (it does).
//
// The remaining flicker sources are:
//   1. Excessive resets — fatih emits "\x1b[0;22m" after every token even if
//      the next token uses the same color.  We merge adjacent same-color spans.
//   2. Lookahead-gated classification — coloring "get" only when "(" follows
//      caused mid-word flicker.  We now classify based on context only, not
//      lookahead, relying on position in the chain instead.
// ──────────────────────────────────────────────────────────────────────────────

const (
	ansiReset    = "\x1b[0m"
	ansiBold     = "\x1b[1m"
	ansiCyan     = "\x1b[36m"
	ansiCyanBold = "\x1b[36;1m"
	ansiMagenta  = "\x1b[35m"
	ansiMagBold  = "\x1b[35;1m"
	ansiGreen    = "\x1b[32m"
	ansiYellow   = "\x1b[33m"
)

func ansiCodeForKind(k syntaxTokenKind) string {
	switch k {
	case syntaxTokenKeyword:
		return ansiCyanBold
	case syntaxTokenMethod:
		return ansiCyan
	case syntaxTokenMeta:
		return ansiMagBold
	case syntaxTokenHelper:
		return ansiMagenta
	case syntaxTokenString:
		return ansiGreen
	case syntaxTokenLiteral:
		return ansiYellow
	default:
		return ""
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// SyntaxPainter
// ──────────────────────────────────────────────────────────────────────────────

// SyntaxPainter applies token-level syntax highlighting to the active input
// line via the readline Painter interface.
type SyntaxPainter struct{}

// NewSyntaxPainter creates a painter for readline live input highlighting.
func NewSyntaxPainter() readline.Painter {
	return &SyntaxPainter{}
}

// Paint colorises tokens while keeping the returned []rune suitable for
// readline's cursor-positioning math.
//
// Key invariant: readline's getBackspaceSequence() counts \b characters based
// on len(r.buf) and r.idx — the original, uncoloured buffer.  It does NOT
// re-measure the Paint output.  So as long as the terminal correctly interprets
// the ANSI escapes we embed (which all modern terminals do), the cursor stays
// in sync regardless of how many extra bytes the escape sequences add.
//
// What Paint must NOT do:
//   - Return extra runes between visible characters that the terminal would
//     render as printable glyphs and therefore advance the cursor.  Plain ASCII
//     bytes that are part of ANSI sequences ("[", "3", "m" etc.) are consumed
//     by the terminal in escape mode and do NOT advance the cursor — so they
//     are safe to include even though they add runes to the returned slice.
//
// What caused the original blinking:
//   - fatih/color wraps every token in open+reset sequences.  Resetting in the
//     middle of a typed word means every keypress toggles between two render
//     states (colored ↔ plain) during the token build-up phase — visible as a
//     flicker.  Merging adjacent same-color spans eliminates the redundant
//     toggles.
//   - Classifying method tokens only when "(" is already present means the
//     color fires on the keystroke that types "(" — one frame the token is
//     plain, the next it flips to cyan.  We now use position-in-chain context
//     instead of lookahead so the color is applied as soon as the identifier is
//     recognized as a method position.
func (p *SyntaxPainter) Paint(line []rune, _ int) []rune {
	tokens := tokenizeSyntax(line)
	if len(tokens) == 0 {
		return line
	}

	var sb strings.Builder
	sb.Grow(len(line) * 2) // rough over-estimate

	activeCode := ""
	for _, tok := range tokens {
		code := ansiCodeForKind(tok.kind)
		if code != activeCode {
			if activeCode != "" {
				sb.WriteString(ansiReset)
			}
			if code != "" {
				sb.WriteString(code)
			}
			activeCode = code
		}
		sb.WriteString(tok.text)
	}
	// Always close any open color at end of line so the terminal doesn't bleed
	// the last color into the prompt on the next render cycle.
	if activeCode != "" {
		sb.WriteString(ansiReset)
	}

	return []rune(sb.String())
}

// ──────────────────────────────────────────────────────────────────────────────
// Tokenizer
// ──────────────────────────────────────────────────────────────────────────────

func tokenizeSyntax(line []rune) []syntaxToken {
	tokens := make([]syntaxToken, 0, len(line))
	firstCommand := ""
	nonSpaceTokenCount := 0

	for i := 0; i < len(line); {
		ch := line[i]

		// ── whitespace ────────────────────────────────────────────────────────
		if unicode.IsSpace(ch) {
			start := i
			for i < len(line) && unicode.IsSpace(line[i]) {
				i++
			}
			tokens = append(tokens, syntaxToken{kind: syntaxTokenPlain, text: string(line[start:i])})
			continue
		}

		// ── quoted string ─────────────────────────────────────────────────────
		if ch == '"' || ch == '\'' {
			start := i
			i++
			escaped := false
			for i < len(line) {
				if escaped {
					escaped = false
					i++
					continue
				}
				if line[i] == '\\' {
					escaped = true
					i++
					continue
				}
				if line[i] == ch {
					i++
					break
				}
				i++
			}
			tokens = append(tokens, syntaxToken{kind: syntaxTokenString, text: string(line[start:i])})
			nonSpaceTokenCount++
			continue
		}

		// ── "?" shorthand ─────────────────────────────────────────────────────
		if ch == '?' {
			kind := syntaxTokenPlain
			if nonSpaceTokenCount == 0 {
				kind = syntaxTokenMeta
				firstCommand = "?"
			}
			tokens = append(tokens, syntaxToken{kind: kind, text: string(ch)})
			i++
			nonSpaceTokenCount++
			continue
		}

		// ── numeric literal ───────────────────────────────────────────────────
		if isNumberStart(line, i) {
			start := i
			i = scanNumber(line, i)
			tokens = append(tokens, syntaxToken{kind: syntaxTokenLiteral, text: string(line[start:i])})
			nonSpaceTokenCount++
			continue
		}

		// ── \o and other backslash-prefixed tokens ────────────────────────────
		if ch == '\\' {
			start := i
			i++
			for i < len(line) && isIdentCharRune(line[i]) {
				i++
			}
			text := string(line[start:i])
			lower := strings.ToLower(text)
			kind := syntaxTokenPlain
			if nonSpaceTokenCount == 0 {
				if _, ok := syntaxMetaWords[lower]; ok {
					kind = syntaxTokenMeta
				}
				firstCommand = lower
			}
			tokens = append(tokens, syntaxToken{kind: kind, text: text})
			nonSpaceTokenCount++
			continue
		}

		// ── identifier ────────────────────────────────────────────────────────
		if isIdentStartRune(ch) {
			start := i
			i++
			for i < len(line) && isIdentCharRune(line[i]) {
				i++
			}
			text := string(line[start:i])
			kind := classifyIdentifierToken(line, start, i, text, firstCommand, nonSpaceTokenCount)
			tokens = append(tokens, syntaxToken{kind: kind, text: text})
			if nonSpaceTokenCount == 0 {
				firstCommand = strings.ToLower(text)
			}
			nonSpaceTokenCount++
			continue
		}

		// ── punctuation / operator ────────────────────────────────────────────
		tokens = append(tokens, syntaxToken{kind: syntaxTokenPlain, text: string(ch)})
		i++
		nonSpaceTokenCount++
	}

	return tokens
}

// classifyIdentifierToken determines the color category of an identifier.
//
// Changes from the original:
//   - Method classification no longer requires the next character to be "(".
//     A word in a dot-separated chain (preceded by ".") is always a method.
//     This eliminates the flicker where typing "db.get(" causes "get" to jump
//     from plain to colored on the "(" keystroke.
//   - Helper classification still checks for "(" because helpers only appear
//     inside argument lists where the paren will usually already be present;
//     when it isn't, falling back to plain is acceptable (no chain position to
//     disambiguate helpers from collection names).
func classifyIdentifierToken(line []rune, start, end int, text, firstCommand string, nonSpaceTokenCount int) syntaxTokenKind {
	lower := strings.ToLower(text)
	prev := prevNonSpaceRune(line, start)
	next := nextNonSpaceRune(line, end)

	// "db" is always a keyword.
	if _, ok := syntaxKeywordWords[lower]; ok {
		return syntaxTokenKeyword
	}

	// true / false / null
	if _, ok := syntaxLiteralWords[lower]; ok {
		return syntaxTokenLiteral
	}

	// FieldValue helpers — require "(" to distinguish from collection names.
	// (When "(" isn't typed yet the token will be plain; that's acceptable
	// because helpers appear inside payloads where the context is unambiguous
	// visually.)
	if _, ok := syntaxHelperWords[lower]; ok && next == '(' {
		return syntaxTokenHelper
	}

	// Method words preceded by "." are always methods — no lookahead needed.
	// We also accept method words that are followed by "(" for cases where
	// the dot was consumed as a separate token before us (shouldn't happen but
	// is a safe fallback).
	if _, ok := syntaxMethodWords[lower]; ok {
		if prev == '.' || next == '(' {
			return syntaxTokenMethod
		}
	}

	// First-position meta commands: help, exit, quit, show, use.
	if nonSpaceTokenCount == 0 {
		if _, ok := syntaxMetaWords[lower]; ok {
			return syntaxTokenMeta
		}
	}

	// "show collections", "show dbs", "show databases"
	if firstCommand == "show" {
		if _, ok := syntaxShowSubcommandWords[lower]; ok {
			return syntaxTokenMeta
		}
	}

	// "\o table|json|pretty"
	if firstCommand == `\o` {
		if _, ok := syntaxOutputFormatWords[lower]; ok {
			return syntaxTokenMeta
		}
	}

	return syntaxTokenPlain
}

// ──────────────────────────────────────────────────────────────────────────────
// Low-level helpers
// ──────────────────────────────────────────────────────────────────────────────

func prevNonSpaceRune(line []rune, idx int) rune {
	for i := idx - 1; i >= 0; i-- {
		if !unicode.IsSpace(line[i]) {
			return line[i]
		}
	}
	return 0
}

func nextNonSpaceRune(line []rune, idx int) rune {
	for i := idx; i < len(line); i++ {
		if !unicode.IsSpace(line[i]) {
			return line[i]
		}
	}
	return 0
}

func isIdentStartRune(ch rune) bool {
	if ch < 0 || ch > 255 {
		return false
	}
	return query.IsIdentStart(byte(ch))
}

func isIdentCharRune(ch rune) bool {
	if ch < 0 || ch > 255 {
		return false
	}
	return query.IsIdentChar(byte(ch))
}

func isNumberStart(line []rune, idx int) bool {
	ch := line[idx]
	if ch == '-' {
		return idx+1 < len(line) && isDigitRune(line[idx+1])
	}
	return isDigitRune(ch)
}

func scanNumber(line []rune, idx int) int {
	i := idx
	if line[i] == '-' {
		i++
	}
	for i < len(line) && isDigitRune(line[i]) {
		i++
	}
	if i+1 < len(line) && line[i] == '.' && isDigitRune(line[i+1]) {
		i++
		for i < len(line) && isDigitRune(line[i]) {
			i++
		}
	}
	return i
}

func isDigitRune(ch rune) bool {
	return ch >= '0' && ch <= '9'
}
