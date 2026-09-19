package content

import (
	"strings"

	"github.com/stricttools/selfdoc/internal/robots"
)

// ResolveListCrawlers produces a Markdown bullet list of every user agent the
// generated robots.txt names, in the order it names them.
//
// The crawler policy is declared once, in package robots, and every robots.txt
// selfdoc writes renders from that declaration. A page that documents the
// policy in prose is a second copy of it: the two go out of step the first time
// a crawler is added, and nothing fails when they do. This directive makes the
// page read the declaration instead.
func ResolveListCrawlers() string {
	lines := make([]string, 0, len(robots.Agents))
	for _, agent := range robots.Agents {
		lines = append(lines, "- `"+agent+"`")
	}
	return strings.Join(lines, "\n")
}
