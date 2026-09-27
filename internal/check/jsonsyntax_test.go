package check

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// jsonConformanceDocuments are the documents the JSON checker is measured
// against CPython on. Each one exercises a distinct refusal of the decoder, so
// a divergence in message or line is a divergence in the diagnostic
// code-block-syntax-error reports.
var jsonConformanceDocuments = []string{
	`{"a": 1}`,
	`{"a": 1,}`,
	`{"a": 1`,
	`{`,
	`{}`,
	`[`,
	`[]`,
	`[1, 2`,
	`[1, 2,]`,
	`[1 2]`,
	`{a: 1}`,
	`{"a" 1}`,
	`{"a": }`,
	`{"a": 1} extra`,
	`"unterminated`,
	`"a` + "\n" + `b"`,
	`"bad \q escape"`,
	`"bad \u12 escape"`,
	`"bad \uZZZZ escape"`,
	`"ok é"`,
	`"surrogate 😀"`,
	`"lone \ud83d tail"`,
	"{\n  \"a\": 1,\n  \"b\": [1, 2,],\n  \"c\": 3\n}",
	"[\n  1,\n  2\n  3\n]",
	`nul`,
	`tru`,
	`-`,
	`01`,
	`1.`,
	`1e`,
	`1e+`,
	`-Infinity`,
	`NaN`,
	`  {"a": 1}  `,
	``,
	`   `,
	"{\n\"key\": \"value with \x01 control\"\n}",
	`{"nested": {"deep": [1, {"x": }]}}`,
	`{"a": 1 "b": 2}`,
	`{"a": 1, "b"}`,
	`{"a": 1, 2: 3}`,
	`{"a": 1 ,}`,
	`[1, 2 ,]`,
	`[1,   ]`,
	`{"a":1,   }`,
	`{"a": 1}x`,
	`[] x`,
	`1 2`,
	`[01]`,
	`{"a": 01}`,
	`"A"`,
	`"😀"`,
	`"\ud83dx"`,
	`"\x41"`,
	`"\u00zz"`,
	`[1, 2,,]`,
	`{"a": 1,,}`,
	`[,,]`,
	`[,]`,
	`{,}`,
	`{"":1,}`,
	`[[1,],]`,
	"\"tab\there\"",
	`"\\"`,
	`true`,
	`False`,
	`{'a': 1}`,
	`"tail \`,
	"{\"a\": 1,\n}",
	"[1, 2,\n]",
}

// pythonJSONVerdict is what CPython's decoder said about one document.
type pythonJSONVerdict struct {
	// OK is true when the document decoded.
	OK bool `json:"ok"`
	// Message is JSONDecodeError.msg, the bare message the rule reports.
	Message string `json:"msg"`
	// Line is JSONDecodeError.lineno.
	Line int `json:"lineno"`
}

// pythonJSONConformanceDriver reads the documents as a JSON array on standard
// input and prints one verdict object per document.
const pythonJSONConformanceDriver = `
import json, sys

documents = json.loads(sys.stdin.read())
verdicts = []
for document in documents:
    try:
        json.loads(document)
    except json.JSONDecodeError as exc:
        verdicts.append({"ok": False, "msg": exc.msg, "lineno": exc.lineno})
    else:
        verdicts.append({"ok": True, "msg": "", "lineno": 0})
sys.stdout.write(json.dumps(verdicts))
`

// TestJSONSyntaxMatchesCPython measures the JSON checker against the decoder
// whose messages code-block-syntax-error reports.
//
// The rule quotes the decoder's own message and blames the decoder's own line,
// so this is the assertion that the Go reimplementation says what CPython
// says. It is the test that makes the reimplementation legitimate rather than
// approximate.
func TestJSONSyntaxMatchesCPython(t *testing.T) {
	requirePython(t)

	encoded, err := json.Marshal(jsonConformanceDocuments)
	if err != nil {
		t.Fatalf("encode documents: %v", err)
	}
	command := exec.Command("python3", "-c", pythonJSONConformanceDriver)
	command.Stdin = strings.NewReader(string(encoded))
	output, err := command.Output()
	if err != nil {
		t.Fatalf("python3 driver: %v", err)
	}
	var expected []pythonJSONVerdict
	if err := json.Unmarshal(output, &expected); err != nil {
		t.Fatalf("decode verdicts: %v", err)
	}
	if len(expected) != len(jsonConformanceDocuments) {
		t.Fatalf("verdicts = %d, want %d", len(expected), len(jsonConformanceDocuments))
	}

	for index, document := range jsonConformanceDocuments {
		want := expected[index]
		failure := CheckJSONSyntax(document)
		if want.OK {
			if failure != nil {
				t.Errorf("document %d (%q): refused with %q at line %d, "+
					"CPython accepted it",
					index, document, failure.Message,
					failure.Line([]rune(document)))
			}
			continue
		}
		if failure == nil {
			t.Errorf("document %d (%q): accepted, CPython refused with %q at line %d",
				index, document, want.Message, want.Line)
			continue
		}
		if failure.Message != want.Message {
			t.Errorf("document %d (%q): message = %q, CPython said %q",
				index, document, failure.Message, want.Message)
		}
		if got := failure.Line([]rune(document)); got != want.Line {
			t.Errorf("document %d (%q): line = %d, CPython said %d",
				index, document, got, want.Line)
		}
	}
}
