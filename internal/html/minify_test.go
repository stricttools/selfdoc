package html

import (
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/js"
)

func TestMinifyJSRemovesComments(t *testing.T) {
	result := MinifyJS("/* header */\nvar x = 1;\n// set y\nvar y = 2;\n")
	mustNotContain(t, result, "/* header */", "set y")
	mustContain(t, result, "x", "y")
}

// A line-initial "//" is a comment whatever punctuation it carries.
//
// The check that keeps a "//" inside a string literal from being stripped
// used to apply to line-initial comments too, so a comment reading "the
// framework's combobox shape" was left in place -- and the whitespace
// collapse then pulled the statement on the NEXT line up onto the
// comment's line, commenting it out along with every block after it in the
// same assembled script. The symptom was a page whose scripts simply did
// not run, with nothing logged anywhere.
func TestMinifyJSStripsACommentContainingAnApostrophe(t *testing.T) {
	result := MinifyJS("// the framework's combobox shape\n(function() { window.ran = true; })();\n")
	mustNotContain(t, result, "//", "combobox")
	if !strings.Contains(strings.ReplaceAll(result, " ", ""), "window.ran=true") {
		t.Errorf("the statement after the comment did not survive:\n%s", result)
	}
}

func TestMinifyJSPreservesURLs(t *testing.T) {
	mustContain(t, MinifyJS("var url = 'https://example.com/path';\n"),
		"https://example.com/path")
}

func TestMinifyJSStripsATrailingCommentWithNoQuote(t *testing.T) {
	result := MinifyJS("var a = 1;  // plain trailing\nvar b = 2;\n")
	mustNotContain(t, result, "plain trailing")
	mustContain(t, result, "var b=2;")
}

func TestMinifyJSKeepsATrailingCommentThatMightBeAString(t *testing.T) {
	// The check is one character wide on purpose: a "//" that follows code
	// and whose line carries a quote might be inside a string literal, so
	// it is left alone rather than guessed at.
	mustContain(t, MinifyJS("var b = 2;  // with 'quote'\n"), "quote")
}

// No block of an assembled script may be swallowed by the comment of the
// block above it.
func TestMinifyJSKeepsEveryBlockOfAnAssembledScriptAlive(t *testing.T) {
	assembled := MinifyJS(js.AssembleBody(js.PageHTML{
		Body:   "<pre>code</pre>",
		Extras: `<div class="tm-notice tm-notice-warn"></div>`,
		Chrome: `<div class="sel version-picker">`,
	}))
	mustNotContain(t, assembled, "//")
	// One IIFE per block, and every one of them outside any comment.
	if n := strings.Count(assembled, "(function(){"); n < 6 {
		t.Errorf("got %d immediately-invoked blocks, want at least 6", n)
	}
}
