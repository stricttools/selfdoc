package content

import (
	"fmt"
	"os"

	"github.com/stricttools/selfdoc/internal/cv"
	"github.com/stricttools/selfdoc/internal/util"
)

// ResolveCV renders the CV declared at the path attribute as the page's body.
//
// Every failure is a hard error naming what is wrong: a missing path, a
// document that is not there, a malformed declaration, or a build with no
// author to state the Person from. A CV page that rendered a placeholder would
// publish a person's record with holes in it.
func ResolveCV(attrs map[string]string, config map[string]any, baseDir string) (string, error) {
	path := attrs["path"]
	if path == "" {
		return "", fmt.Errorf(
			`directive 'cv' requires path="<file>": the document that ` +
				"declares the curriculum vitae, e.g. docs/cv.toml.",
		)
	}
	fullPath := util.ResolveDirectivePath(baseDir, path)
	if info, err := os.Stat(fullPath); err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf(
			"directive 'cv': %s is not a file. The CV is declared in a TOML "+
				"document at that path, relative to the project root.",
			util.PythonRepr(path),
		)
	}
	document, err := cv.LoadCV(fullPath)
	if err != nil {
		return "", err
	}
	author, _ := config["author"].(map[string]any)
	return cv.RenderCVPage(document, author)
}
