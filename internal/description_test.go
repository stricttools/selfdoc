// This file holds the module-wide invariant that selfdoc says the same thing
// about itself everywhere it says anything at all.
package internal_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// projectDescription reads the one line the repository's own selfdoc.json
// declares, which is the sentence every other statement of what selfdoc is
// has to open with.
func projectDescription(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(moduleRoot(t), "selfdoc.json"))
	if err != nil {
		t.Fatalf("reading selfdoc.json: %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("parsing selfdoc.json: %v", err)
	}
	description, _ := document["description"].(string)
	if description == "" {
		t.Fatal("selfdoc.json declares no description")
	}
	return description
}

// TestTheReadmeOpensWithTheProjectDescription asserts the README template's
// first paragraph opens with the declared description, so a reader arriving at
// the repository is told what the project's own config says it is.
func TestTheReadmeOpensWithTheProjectDescription(t *testing.T) {
	description := projectDescription(t)
	data, err := os.ReadFile(filepath.Join(
		moduleRoot(t), "stricttools", "docs", "_README.md"))
	if err != nil {
		t.Fatalf("reading the README template: %v", err)
	}
	paragraph := firstProseParagraph(t, string(data))
	if !strings.HasPrefix(paragraph, description) {
		t.Errorf("the README template's first paragraph is\n  %q\n"+
			"which does not open with the declared description\n  %q",
			paragraph, description)
	}
}

// TestTheCommandDocOpensWithTheProjectDescription asserts the doc comment on
// the module root -- the command, which is what the module root is -- opens
// with the declared description.
func TestTheCommandDocOpensWithTheProjectDescription(t *testing.T) {
	description := projectDescription(t)
	data, err := os.ReadFile(filepath.Join(moduleRoot(t), "main.go"))
	if err != nil {
		t.Fatalf("reading main.go: %v", err)
	}
	var comment []string
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "//") {
			break
		}
		comment = append(comment, strings.TrimSpace(strings.TrimPrefix(line, "//")))
	}
	doc := strings.Join(comment, " ")
	want := "Command selfdoc: " + strings.TrimSuffix(description, ".") + "."
	if !strings.HasPrefix(doc, want) {
		t.Errorf("the command's doc comment opens with\n  %q\nwant\n  %q",
			doc, want)
	}
}

// firstProseParagraph is the first paragraph of a Markdown document that is
// neither frontmatter nor a heading.
func firstProseParagraph(t *testing.T, document string) string {
	t.Helper()
	body := document
	if strings.HasPrefix(body, "+++\n") {
		if end := strings.Index(body[4:], "\n+++\n"); end >= 0 {
			body = body[4+end+len("\n+++\n"):]
		}
	}
	for _, block := range strings.Split(body, "\n\n") {
		block = strings.TrimSpace(block)
		if block == "" || strings.HasPrefix(block, "#") {
			continue
		}
		return block
	}
	t.Fatal("the document carries no prose paragraph")
	return ""
}
