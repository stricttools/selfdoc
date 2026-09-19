package svelte

import (
	"regexp"
	"strings"

	"github.com/stricttools/selfdoc/internal/extractors/typescript"
)

var (
	// moduleScript matches a module script block, in either of the two
	// spellings Svelte has had: the context attribute and the bare marker.
	moduleScript = regexp.MustCompile(
		`(?s)<script\b[^>]*(?:context=["']module["']|\bmodule\b)[^>]*>(.*?)</script>`)

	// anyScript matches a script block with any attributes at all.
	anyScript = regexp.MustCompile(`(?s)<script\b[^>]*>(.*?)</script>`)

	// moduleWord matches the module marker inside an opening tag.
	moduleWord = regexp.MustCompile(`\bmodule\b`)

	// componentDoc matches the JSDoc block at the very start of a script
	// block, which is where a component's own documentation is written.
	componentDoc = regexp.MustCompile(`(?s)^` + pySpace + `*/\*\*` + pySpace + `*\n(.*?)\*/`)
)

// scriptBlocks are a component's two script blocks: the instance script, which
// runs once per component instance, and the module script, which runs once for
// the module.
type scriptBlocks struct {
	Instance string
	Module   string
}

// extractScriptBlocks reads a component's script blocks out of its source.
//
// The instance script is the first script block that is not the module script.
// A component with neither answers with two empty strings, which every caller
// treats as "nothing declared here".
func extractScriptBlocks(source string) scriptBlocks {
	var blocks scriptBlocks

	if m := moduleScript.FindStringSubmatch(source); m != nil {
		blocks.Module = m[1]
	}

	for _, m := range anyScript.FindAllStringSubmatch(source, -1) {
		tag := m[0]
		openingTag := strings.SplitN(tag, ">", 2)[0]
		if strings.Contains(tag, `context="module"`) ||
			strings.Contains(tag, "context='module'") ||
			moduleWord.MatchString(openingTag) {
			continue
		}
		blocks.Instance = m[1]
		break
	}

	return blocks
}

// extractComponentDoc is the component's own documentation: the JSDoc block at
// the top of the instance script, before any code. It is the empty string when
// the component carries none.
func extractComponentDoc(source string) string {
	instance := extractScriptBlocks(source).Instance
	if instance == "" {
		return ""
	}
	match := componentDoc.FindStringSubmatch(instance)
	if match == nil {
		return ""
	}
	return typescript.ParseJSDocText(match[1]).Description
}
