package preview

import (
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/blog/verify"
)

// The report is printed first and loudly, and says out loud that the server
// starts anyway -- a preview exists to be looked at when something is wrong.

func TestRenderReport(t *testing.T) {
	t.Run("a clean report names the tree and the counts", func(t *testing.T) {
		report := &verify.VerifyReport{Ran: []string{"page-metadata", "site-chrome"}}
		text := RenderReport(report, "/somewhere/out")
		if !strings.Contains(text, "verify: /somewhere/out") {
			t.Errorf("the report does not name the tree:\n%s", text)
		}
		if !strings.Contains(text, "check(s) ran") {
			t.Errorf("the report does not count the checks:\n%s", text)
		}
		if !strings.Contains(text, "Every check that ran passed.") {
			t.Errorf("the clean report does not say so:\n%s", text)
		}
		if !strings.Contains(text, strings.Repeat("=", 72)) {
			t.Errorf("the report carries no rule:\n%s", text)
		}
	})

	t.Run("the counts are the checks that ran against the whole set", func(t *testing.T) {
		report := &verify.VerifyReport{Ran: []string{"page-metadata"}}
		text := RenderReport(report, "/somewhere/out")
		first := strings.Split(text, "\n")[2]
		want := "  1 of " + itoa(len(verify.Checks)) + " check(s) ran, 0 problem(s) found."
		if first != want {
			t.Errorf("the count line is %q, want %q", first, want)
		}
	})

	t.Run("a skipped check says what was missing", func(t *testing.T) {
		report := &verify.VerifyReport{
			Ran:     []string{"page-metadata"},
			Skipped: []verify.Skip{{Check: "outbound-links", Reason: "no outbound.toml"}},
		}
		text := RenderReport(report, "/somewhere/out")
		if !strings.Contains(text, "  NOT CHECKED: outbound-links -- no outbound.toml") {
			t.Errorf("the report does not name the skip:\n%s", text)
		}
	})

	t.Run("a failing report says the preview serves it anyway", func(t *testing.T) {
		report := &verify.VerifyReport{
			Failures: []verify.Failure{{
				Check: "page-metadata", Offender: "site/x.html", Message: "no title",
			}},
			Ran: []string{"page-metadata"},
		}
		text := RenderReport(report, "/tmp/x")
		if !strings.Contains(text, "no title") {
			t.Errorf("the report does not carry the failure:\n%s", text)
		}
		if !strings.Contains(text, "serves this tree anyway") {
			t.Errorf("the report does not say the server starts:\n%s", text)
		}
		if !strings.Contains(text, "A deploy would refuse it.") {
			t.Errorf("the report does not say a deploy would refuse:\n%s", text)
		}
	})
}
