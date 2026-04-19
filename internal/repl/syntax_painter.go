package repl

import (
	"strings"
	"unicode"

	"github.com/chzyer/readline"
	"github.com/fatih/color"
	"github.com/tomas-santana/firesh/internal/query"
)

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

var syntaxKeywordColor = color.New(color.FgCyan, color.Bold).SprintFunc()
var syntaxMethodColor = color.New(color.FgCyan).SprintFunc()
var syntaxMetaColor = color.New(color.FgMagenta, color.Bold).SprintFunc()
var syntaxHelperColor = color.New(color.FgMagenta).SprintFunc()
var syntaxStringColor = color.New(color.FgGreen).SprintFunc()
var syntaxLiteralColor = color.New(color.FgYellow).SprintFunc()

// SyntaxPainter applies token-level highlighting to the active input line.
type SyntaxPainter struct{}

// NewSyntaxPainter creates a painter for readline live input highlighting.
func NewSyntaxPainter() readline.Painter {
	return &SyntaxPainter{}
}

// Paint colorises tokens while preserving the original rune content.
func (p *SyntaxPainter) Paint(line []rune, _ int) []rune {
	tokens := tokenizeSyntax(line)
	if len(tokens) == 0 {
		return line
	}

	var out strings.Builder
	for _, tok := range tokens {
		out.WriteString(colorizeSyntaxToken(tok))
	}
	return []rune(out.String())
}

func colorizeSyntaxToken(tok syntaxToken) string {
	switch tok.kind {
	case syntaxTokenKeyword:
		return syntaxKeywordColor(tok.text)
	case syntaxTokenMethod:
		return syntaxMethodColor(tok.text)
	case syntaxTokenMeta:
		return syntaxMetaColor(tok.text)
	case syntaxTokenHelper:
		return syntaxHelperColor(tok.text)
	case syntaxTokenString:
		return syntaxStringColor(tok.text)
	case syntaxTokenLiteral:
		return syntaxLiteralColor(tok.text)
	default:
		return tok.text
	}
}

func tokenizeSyntax(line []rune) []syntaxToken {
	tokens := make([]syntaxToken, 0, len(line))
	firstCommand := ""
	nonSpaceTokenCount := 0

	for i := 0; i < len(line); {
		ch := line[i]

		if unicode.IsSpace(ch) {
			start := i
			for i < len(line) && unicode.IsSpace(line[i]) {
				i++
			}
			tokens = append(tokens, syntaxToken{kind: syntaxTokenPlain, text: string(line[start:i])})
			continue
		}

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

		if isNumberStart(line, i) {
			start := i
			i = scanNumber(line, i)
			tokens = append(tokens, syntaxToken{kind: syntaxTokenLiteral, text: string(line[start:i])})
			nonSpaceTokenCount++
			continue
		}

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

		tokens = append(tokens, syntaxToken{kind: syntaxTokenPlain, text: string(ch)})
		i++
		nonSpaceTokenCount++
	}

	return tokens
}

func classifyIdentifierToken(line []rune, start, end int, text, firstCommand string, nonSpaceTokenCount int) syntaxTokenKind {
	lower := strings.ToLower(text)
	prev := prevNonSpaceRune(line, start)
	next := nextNonSpaceRune(line, end)

	if _, ok := syntaxKeywordWords[lower]; ok {
		return syntaxTokenKeyword
	}
	if _, ok := syntaxLiteralWords[lower]; ok {
		return syntaxTokenLiteral
	}
	if _, ok := syntaxHelperWords[lower]; ok && next == '(' {
		return syntaxTokenHelper
	}
	if _, ok := syntaxMethodWords[lower]; ok && (prev == '.' || next == '(') {
		return syntaxTokenMethod
	}
	if nonSpaceTokenCount == 0 {
		if _, ok := syntaxMetaWords[lower]; ok {
			return syntaxTokenMeta
		}
	}
	if firstCommand == "show" {
		if _, ok := syntaxShowSubcommandWords[lower]; ok {
			return syntaxTokenMeta
		}
	}
	if firstCommand == `\o` {
		if _, ok := syntaxOutputFormatWords[lower]; ok {
			return syntaxTokenMeta
		}
	}

	return syntaxTokenPlain
}

func prevNonSpaceRune(line []rune, idx int) rune {
	for i := idx - 1; i >= 0; i-- {
		if unicode.IsSpace(line[i]) {
			continue
		}
		return line[i]
	}
	return 0
}

func nextNonSpaceRune(line []rune, idx int) rune {
	for i := idx; i < len(line); i++ {
		if unicode.IsSpace(line[i]) {
			continue
		}
		return line[i]
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
