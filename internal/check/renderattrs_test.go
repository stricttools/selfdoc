package check

import (
	"testing"

	"github.com/stricttools/selfdoc/internal/directives"
)

// The report quotes a directive in the spelling its author typed, attributes
// in source order.
func TestRenderAttrsFollowsSourceOrder(t *testing.T) {
	parsed, err := directives.ParseDirectives(`:-: ref path="." lang="go"`, nil)
	if err != nil {
		t.Fatalf("ParseDirectives: %v", err)
	}
	got := renderAttrs(parsed[0].Attrs, parsed[0].AttrOrder)
	want := `path="." lang="go"`
	if got != want {
		t.Errorf("renderAttrs = %q, want %q", got, want)
	}
}

// A directive with no attributes renders as the empty string, so the report
// line is the bare name with no trailing space.
func TestRenderAttrsEmpty(t *testing.T) {
	if got := renderAttrs(map[string]string{}, nil); got != "" {
		t.Errorf("renderAttrs = %q, want empty", got)
	}
}

// An order list that does not cover every key still renders every key: the
// uncovered ones follow, sorted, so nothing is silently dropped.
func TestRenderAttrsCoversKeysMissingFromTheOrder(t *testing.T) {
	attrs := map[string]string{"b": "2", "a": "1", "c": "3"}
	got := renderAttrs(attrs, []string{"b"})
	want := `b="2" a="1" c="3"`
	if got != want {
		t.Errorf("renderAttrs = %q, want %q", got, want)
	}
}
