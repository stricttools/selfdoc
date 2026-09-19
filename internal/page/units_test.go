package page

import (
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/address"
	"github.com/stricttools/selfdoc/internal/util"
)

// TestDateRenderingMatchesPythonStrptime covers what the footer and the
// content header do with a date frontmatter can carry: an unpadded field is a
// date (Python's month and day directives accept one), a four-digit year is
// required, and anything that is not a date is printed as written rather than
// swallowed.
func TestDateRenderingMatchesPythonStrptime(t *testing.T) {
	cases := map[string]string{
		"2026-06-29": "June 29, 2026",
		"2026-1-5":   "January 5, 2026",
		"2026-13-01": "2026-13-01",
		"2026-02-30": "2026-02-30",
		"26-1-1":     "26-1-1",
		"2026-1-5 ":  "2026-1-5 ",
		" 2026-1-5":  " 2026-1-5",
		"2026-0-1":   "2026-0-1",
		"tomorrow":   "tomorrow",
		"":           "",
	}
	for value, want := range cases {
		if got := formatDateModified(value); got != want {
			t.Errorf("formatDateModified(%q) = %q, want %q", value, got, want)
		}
	}
}

// TestPythonCapitalizeIsNotTitleCase covers the breadcrumb and
// BreadcrumbList labels: a hyphen or an underscore is uncased, so the
// segment after it is NOT capitalized, which is where Python's capitalize and
// title differ.
func TestPythonCapitalizeIsNotTitleCase(t *testing.T) {
	cases := map[string]string{
		"api":            "Api",
		"get-started":    "Get-started",
		"user_guide":     "User_guide",
		"API":            "Api",
		"éclair":         "Éclair",
		"":               "",
		"a":              "A",
		"multiple words": "Multiple words",
	}
	for value, want := range cases {
		if got := pythonCapitalize(value); got != want {
			t.Errorf("pythonCapitalize(%q) = %q, want %q", value, got, want)
		}
	}
	if util.TitleCase("get-started") == pythonCapitalize("get-started") {
		t.Fatal("capitalize and title must differ on a hyphenated segment")
	}
}

// TestNumericFrontmatterAcceptsWhatPythonAccepts covers the sort keys read
// off frontmatter, where Python's isinstance check treats a bool as a number
// because bool subclasses int there.
func TestNumericFrontmatterAcceptsWhatPythonAccepts(t *testing.T) {
	cases := []struct {
		value any
		want  float64
		ok    bool
	}{
		{int64(3), 3, true},
		{3, 3, true},
		{2.5, 2.5, true},
		{true, 1, true},
		{false, 0, true},
		{"3", 0, false},
		{nil, 0, false},
	}
	for _, c := range cases {
		got, ok := fmNumberOK(util.Frontmatter{"nav_order": c.value}, "nav_order")
		if ok != c.ok || got != c.want {
			t.Errorf("fmNumberOK(%#v) = (%v, %v), want (%v, %v)",
				c.value, got, ok, c.want, c.ok)
		}
	}
	if _, ok := fmNumberOK(util.Frontmatter{}, "order"); ok {
		t.Error("an absent key must report no number")
	}
}

// TestPageTagsCoerceAStringToOneItem covers the tags facet, where a bare
// string and a one-item list mean the same thing and an empty string means no
// tags at all.
func TestPageTagsCoerceAStringToOneItem(t *testing.T) {
	if got := fmStrings(util.Frontmatter{"tags": "release"}, "tags"); len(got) != 1 ||
		got[0] != "release" {
		t.Errorf("a bare string tag became %v", got)
	}
	if got := fmStrings(util.Frontmatter{"tags": ""}, "tags"); len(got) != 0 {
		t.Errorf("an empty tag string became %v", got)
	}
	if got := fmStrings(util.Frontmatter{"tags": []string{"a", "b"}}, "tags"); len(got) != 2 {
		t.Errorf("a tag list became %v", got)
	}
	if got := fmStrings(util.Frontmatter{}, "tags"); len(got) != 0 {
		t.Errorf("an absent tags key became %v", got)
	}
}

// TestShareControlNeedsAnAbsoluteAddress covers the one case where the share
// control renders nothing despite the page having a version: a shared link
// leaves the site, so with no base URL and no URL builder there is no address
// to offer.
func TestShareControlNeedsAnAbsoluteAddress(t *testing.T) {
	addr, err := address.NewPageAddress("guide/index.html", address.Coordinates{
		Version: "1.0.0",
	})
	if err != nil {
		t.Fatalf("NewPageAddress: %v", err)
	}
	if got := renderShareControl(addr, nil, ""); got != "" {
		t.Fatalf("with no absolute address the control must render nothing, got %q", got)
	}
	got := renderShareControl(addr, nil, "https://example.com")
	if !strings.Contains(got, "Evergreen link (always current)") {
		t.Fatal("the evergreen choice is missing")
	}
	// The current version is emitted at the stable address and nowhere
	// else, so offering its pinned address would hand the reader a 404.
	if strings.Contains(got, "Pinned link") {
		t.Fatal("the current version must not offer a pinned address")
	}

	archived, err := address.NewPageAddress("guide/index.html", address.Coordinates{
		Version: "1.0.0", Archived: true,
	})
	if err != nil {
		t.Fatalf("NewPageAddress: %v", err)
	}
	got = renderShareControl(archived, nil, "https://example.com")
	if !strings.Contains(got, "Pinned link (v1.0.0)") {
		t.Fatal("an archive page must offer its pinned address")
	}
}

// TestAPageWithNoVersionHasNoShareControl covers the precondition: the
// control names version-scoped addresses, so a page that is not
// version-scoped has nothing to offer.
func TestAPageWithNoVersionHasNoShareControl(t *testing.T) {
	addr, err := address.NewPageAddress("guide/index.html", address.Coordinates{})
	if err != nil {
		t.Fatalf("NewPageAddress: %v", err)
	}
	if got := renderShareControl(addr, nil, "https://example.com"); got != "" {
		t.Fatalf("an unversioned page must render no share control, got %q", got)
	}
}

// TestAHandwrittenDescriptionIsEmittedVerbatim covers the abolition of
// truncation: a frontmatter description is a complete linguistic unit and the
// meta tag carries it unchanged, with no cap and no synthesized ellipsis.
func TestAHandwrittenDescriptionIsEmittedVerbatim(t *testing.T) {
	long := strings.Repeat("A", 50) + " " + strings.Repeat("B", 50) + " " +
		strings.Repeat("C", 50) + " " + strings.Repeat("D", 50) + " " +
		strings.Repeat("E", 50) + " " + strings.Repeat("F", 50)
	opts := baseOptions(src("index.md", "# Test\n\nSome content here.\n"))
	opts.Frontmatter = map[string]util.Frontmatter{
		"index.md": {"description": long},
	}
	files, err := GenerateHTML(opts)
	if err != nil {
		t.Fatalf("GenerateHTML: %v", err)
	}
	want := `<meta name="description" content="` + long + `">`
	if !strings.Contains(files["index.html"], want) {
		t.Fatal("the description must reach the meta tag unchanged")
	}
	if strings.Contains(files["index.html"], `content="`+long[:20]) &&
		strings.Contains(files["index.html"], `...">`) {
		t.Fatal("an ellipsis was synthesized")
	}
}

// TestPickerIDsAreUniqueWithinAPage covers the shape the framework's markup
// contract pins: the button's aria-controls names its own listbox, so two
// pickers on one page cannot share an id.
func TestPickerIDsAreUniqueWithinAPage(t *testing.T) {
	resetSelectCounter()
	first := renderSelect("version-picker", "Documentation version",
		[]selectOption{{value: "1.0.0", href: "./", text: "v1.0.0", selected: true}})
	second := renderSelect("locale-picker", "Language",
		[]selectOption{{value: "en", href: "./", text: "English", selected: true}})
	if !strings.Contains(first, `aria-controls="tm-sel-1-listbox"`) ||
		!strings.Contains(first, `id="tm-sel-1-listbox"`) {
		t.Fatalf("the first picker's ids are wrong:\n%s", first)
	}
	if !strings.Contains(second, `aria-controls="tm-sel-2-listbox"`) ||
		!strings.Contains(second, `id="tm-sel-2-listbox"`) {
		t.Fatalf("the second picker's ids are wrong:\n%s", second)
	}
}

// TestTheSelectedOptionNamesTheButtonsLabel covers the fallback: with no
// option selected the button shows the first one, which is what a control
// rendered before any state exists has to say.
func TestTheSelectedOptionNamesTheButtonsLabel(t *testing.T) {
	resetSelectCounter()
	rendered := renderSelect("version-picker", "Documentation version",
		[]selectOption{
			{value: "0.9.0", href: "v/0.9.0/", text: "v0.9.0"},
			{value: "1.0.0", href: "./", text: "v1.0.0"},
		})
	if !strings.Contains(rendered, `<span class="sel-label">v0.9.0</span>`) {
		t.Fatalf("with nothing selected the first option must label the button:\n%s",
			rendered)
	}
	resetSelectCounter()
	empty := renderSelect("version-picker", "Documentation version", nil)
	if !strings.Contains(empty, `<span class="sel-label"></span>`) {
		t.Fatalf("an empty picker must carry an empty label:\n%s", empty)
	}
}
