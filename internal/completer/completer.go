// Package completer provides readline tab completion for firesh commands.
package completer

import (
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/chzyer/readline"
	"github.com/tomas-santana/firesh/internal/query"
)

var topLevelSuggestions = []string{
	"db.",
	"help",
	"exit",
	"quit",
	"show ",
	"use ",
	`\o `,
}

var showSuggestions = []string{
	"collections",
	"dbs",
	"databases",
}

var outputFormatSuggestions = []string{
	"table",
	"json",
	"pretty",
}

var chainMethodSuggestions = []string{
	"doc(",
	"where(",
	"whereOr(",
	"orderBy(",
	"limit(",
	"offset(",
	"select(",
	"get()",
	"watch()",
	"add(",
	"set(",
	"update(",
	"delete(",
	"aggregate(",
}

var valueSuggestions = []string{
	"true",
	"false",
	"null",
	"serverTimestamp()",
	"deleteField()",
	"arrayUnion(",
	"arrayRemove(",
	"increment(",
	"count()",
	"sum(",
	"avg(",
}

// Completer implements readline.AutoCompleter.
type Completer struct{}

// New returns a firesh readline completer.
func New() readline.AutoCompleter {
	return &Completer{}
}

// Do returns suffix candidates and the length of the token to be replaced.
func (c *Completer) Do(line []rune, pos int) ([][]rune, int) {
	if pos < 0 {
		pos = 0
	}
	if pos > len(line) {
		pos = len(line)
	}

	input := string(line[:pos])
	if c.inQuotedString(input) {
		return nil, 0
	}

	token, _, tokenRuneLen := c.currentToken(input)
	if tokenRuneLen == 0 && strings.TrimSpace(input) == "" {
		return c.toSuffixes(topLevelSuggestions, ""), 0
	}

	candidates := c.candidatesFor(input)
	if len(candidates) == 0 {
		return nil, tokenRuneLen
	}
	return c.toSuffixes(candidates, token), tokenRuneLen
}

func (c *Completer) candidatesFor(input string) []string {
	trimmed := strings.TrimLeft(input, " \t")
	if trimmed == "" {
		return topLevelSuggestions
	}

	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "show") {
		if c.isSingleToken(trimmed) {
			return []string{"show "}
		}
		if strings.HasPrefix(lower, "show ") {
			return showSuggestions
		}
	}

	if strings.HasPrefix(lower, `\o`) {
		if c.isSingleToken(trimmed) {
			return []string{`\o `}
		}
		if strings.HasPrefix(lower, `\o `) {
			return outputFormatSuggestions
		}
	}

	if strings.HasPrefix(lower, "use") {
		if c.isSingleToken(trimmed) {
			return []string{"use "}
		}
		return nil
	}

	if strings.HasPrefix(lower, "db") {
		return c.dbSuggestions(trimmed)
	}

	return topLevelSuggestions
}

func (c *Completer) dbSuggestions(input string) []string {
	lower := strings.ToLower(input)
	if lower == "db" {
		return []string{"db."}
	}
	if !strings.HasPrefix(lower, "db.") {
		return []string{"db."}
	}

	token, tokenStart, _ := c.currentToken(input)
	_ = token
	prev := c.prevNonSpace(input, tokenStart)

	switch prev {
	case '.':
		before := strings.ToLower(strings.TrimSpace(input[:tokenStart]))
		if before == "db." {
			return []string{"collectionGroup("}
		}
		return chainMethodSuggestions
	case ':', ',', '(', '[':
		return valueSuggestions
	default:
		return nil
	}
}

func (c *Completer) toSuffixes(candidates []string, token string) [][]rune {
	tokenLower := strings.ToLower(token)
	seen := map[string]struct{}{}
	matched := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if !strings.HasPrefix(strings.ToLower(candidate), tokenLower) {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		matched = append(matched, candidate)
	}
	if len(matched) == 0 {
		return nil
	}

	sort.Strings(matched)
	tokenByteLen := len(token)
	out := make([][]rune, 0, len(matched))
	for _, candidate := range matched {
		if len(candidate) < tokenByteLen {
			continue
		}
		out = append(out, []rune(candidate[tokenByteLen:]))
	}
	return out
}

func (c *Completer) currentToken(input string) (token string, start int, runeLen int) {
	start = len(input)
	for start > 0 {
		ch := input[start-1]
		if !c.isTokenChar(ch) {
			break
		}
		start--
	}
	token = input[start:]
	return token, start, utf8.RuneCountInString(token)
}

func (c *Completer) isTokenChar(ch byte) bool {
	return ch == '\\' || query.IsIdentChar(ch)
}

func (c *Completer) prevNonSpace(input string, idx int) byte {
	for i := idx - 1; i >= 0; i-- {
		if input[i] == ' ' || input[i] == '\t' {
			continue
		}
		return input[i]
	}
	return 0
}

func (c *Completer) isSingleToken(input string) bool {
	for i := 0; i < len(input); i++ {
		if input[i] == ' ' || input[i] == '\t' {
			return false
		}
	}
	return true
}

func (c *Completer) inQuotedString(input string) bool {
	inSingle := false
	inDouble := false
	escaped := false

	for i := 0; i < len(input); i++ {
		ch := input[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
		}
	}

	return inSingle || inDouble
}
