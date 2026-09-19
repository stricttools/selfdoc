package check

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/extractors"
	"github.com/stricttools/selfdoc/internal/lints"
)

// pythonSourceEntry is the declared source entry the PARAM001/RETURN001 cases
// resolve their directive through.
func pythonSourceEntry(t *testing.T) *extractors.SourceEntry {
	t.Helper()
	extractor, present, err := extractors.Lookup("python")
	if err != nil {
		t.Fatalf("Lookup python: %v", err)
	}
	if !present {
		t.Skip("the Python extractor is not linked into this binary")
	}
	return &extractors.SourceEntry{
		Path: "src/", Language: "python", Extractor: extractor,
	}
}

// symbolDocsCase is one PARAM001/RETURN001 scenario.
type symbolDocsCase struct {
	// name identifies the subtest.
	name string
	// source is the module the directive documents.
	source string
	// target is the symbol the directive names, "" for a directive with
	// none.
	target string
	// want are the codes the rules must emit.
	want []string
	// absent are the codes the rules must not emit.
	absent []string
	// fragment is a string the first diagnostic must carry.
	fragment string
}

func runSymbolDocsCases(t *testing.T, cases []symbolDocsCase) {
	t.Helper()
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := lintProject(t)
			write(t, filepath.Join(fixture.Root, "src", "mod.py"), testCase.source)
			write(t, filepath.Join(fixture.DocsDir, "page.md"),
				"+++\ndescription = \"A page about greet\"\n+++\n# Greet\n\nContent.\n")

			attrs := map[string]string{"path": "mod"}
			if testCase.target != "" {
				attrs["target"] = testCase.target
			}
			resolved := []ResolvedDirective{{
				Name:        "ref",
				Attrs:       attrs,
				Content:     "def greet(...)",
				File:        "page.md",
				SourceEntry: pythonSourceEntry(t),
			}}

			results := runLintsOn(t, fixture, resolved)
			for _, code := range testCase.want {
				if !hasCode(results, code) {
					t.Errorf("%s missing; got %v", code, messagesOf(results))
				}
			}
			for _, code := range testCase.absent {
				if hasCode(results, code) {
					t.Errorf("%s fired: %v", code,
						messagesOf(withCode(results, code)))
				}
			}
			if testCase.fragment == "" {
				return
			}
			var matching []lints.LintResult
			for _, code := range testCase.want {
				matching = append(matching, withCode(results, code)...)
			}
			if len(matching) == 0 {
				return
			}
			if !strings.Contains(matching[0].Message(), testCase.fragment) {
				t.Errorf("message = %q, want it to carry %q",
					matching[0].Message(), testCase.fragment)
			}
		})
	}
}

func TestPARAM001(t *testing.T) {
	runSymbolDocsCases(t, []symbolDocsCase{
		{
			name: "a parameter the docstring never names",
			source: `def greet(name: str, greeting: str, loud: bool = False) -> str:
    """Say hello.

    Args:
        name: The person to greet.
        greeting: The greeting to use.

    Returns:
        The greeting.
    """
    pass
`,
			target:   "greet",
			want:     []string{"PARAM001"},
			fragment: "loud",
		},
		{
			name: "every parameter documented",
			source: `def greet(name: str, greeting: str) -> str:
    """Say hello.

    Args:
        name: The person to greet.
        greeting: The greeting to use.

    Returns:
        The greeting.
    """
    pass
`,
			target: "greet",
			absent: []string{"PARAM001"},
		},
		{
			name: "a function with no parameters",
			source: `def greet() -> str:
    """Say hello.

    Returns:
        The greeting.
    """
    pass
`,
			target: "greet",
			absent: []string{"PARAM001"},
		},
		{
			name: "a directive naming no target",
			source: `def greet(name: str) -> str:
    """Say hello."""
    pass
`,
			absent: []string{"PARAM001", "RETURN001"},
		},
	})
}

func TestRETURN001(t *testing.T) {
	runSymbolDocsCases(t, []symbolDocsCase{
		{
			name: "a return value the docstring never describes",
			source: `def greet(name: str) -> str:
    """Say hello.

    Args:
        name: The person to greet.
    """
    pass
`,
			target:   "greet",
			want:     []string{"RETURN001"},
			fragment: "str",
		},
		{
			name: "a documented return value",
			source: `def greet(name: str) -> str:
    """Say hello.

    Args:
        name: The person to greet.

    Returns:
        The greeting text.
    """
    pass
`,
			target: "greet",
			absent: []string{"RETURN001"},
		},
		{
			name: "no declared return type",
			source: `def greet(name):
    """Say hello.

    Args:
        name: The person to greet.
    """
    pass
`,
			target: "greet",
			absent: []string{"RETURN001"},
		},
		{
			name: "a None return type is nothing to document",
			source: `def greet(name: str) -> None:
    """Say hello.

    Args:
        name: The person to greet.
    """
    pass
`,
			target: "greet",
			absent: []string{"RETURN001"},
		},
	})
}
