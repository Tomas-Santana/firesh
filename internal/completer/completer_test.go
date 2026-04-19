package completer

import (
	"sort"
	"testing"
)

func TestTopLevelCompletion(t *testing.T) {
	c := New()
	got := completedCandidates(c, "he")
	if !contains(got, "help") {
		t.Fatalf("expected help completion, got: %v", got)
	}
}

func TestShowCompletion(t *testing.T) {
	c := New()
	got := completedCandidates(c, "show d")
	if !contains(got, "dbs") {
		t.Fatalf("expected dbs completion, got: %v", got)
	}
	if !contains(got, "databases") {
		t.Fatalf("expected databases completion, got: %v", got)
	}
}

func TestChainMethodCompletion(t *testing.T) {
	c := New()
	got := completedCandidates(c, "db.users.w")
	if !contains(got, "where(") {
		t.Fatalf("expected where( completion, got: %v", got)
	}
	if !contains(got, "whereOr(") {
		t.Fatalf("expected whereOr( completion, got: %v", got)
	}
	if !contains(got, "watch()") {
		t.Fatalf("expected watch() completion, got: %v", got)
	}
}

func TestFieldValueCompletion(t *testing.T) {
	c := New()
	got := completedCandidates(c, "db.users.update({lastSeen: ser")
	if !contains(got, "serverTimestamp()") {
		t.Fatalf("expected serverTimestamp() completion, got: %v", got)
	}
}

func TestNoCompletionInsideQuotes(t *testing.T) {
	c := New()
	items, _ := c.Do([]rune(`db.users.where("na`), len([]rune(`db.users.where("na`)))
	if len(items) != 0 {
		t.Fatalf("expected no completions inside quoted string, got: %v", items)
	}
}

func completedCandidates(c interface {
	Do([]rune, int) ([][]rune, int)
}, input string) []string {
	runes := []rune(input)
	suffixes, replaceLen := c.Do(runes, len(runes))
	if replaceLen < 0 {
		replaceLen = 0
	}
	if replaceLen > len(runes) {
		replaceLen = len(runes)
	}
	prefix := string(runes[len(runes)-replaceLen:])

	out := make([]string, 0, len(suffixes))
	for _, suffix := range suffixes {
		out = append(out, prefix+string(suffix))
	}
	sort.Strings(out)
	return out
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
