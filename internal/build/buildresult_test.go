package build

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/page"
	"github.com/stricttools/selfdoc/internal/themes"
	"github.com/stricttools/selfdoc/internal/urls"
	"github.com/stricttools/selfdoc/internal/util"
)

func TestBuildResultCarriesEveryField(t *testing.T) {
	t.Parallel()
	meta := themes.Metadata{Name: "default"}
	builder := urls.NewSimpleURLBuilder("https://example.com")
	result := BuildResult{
		HTMLFiles:         map[string]string{"index.html": "<h1>Hi</h1>"},
		MarkdownFiles:     []page.SourceFile{{MdPath: "index.md", Content: "# Hi"}},
		Frontmatter:       map[string]util.Frontmatter{"index.md": {"title": "Hi"}},
		PageDates:         map[string]page.PageDates{"index.md": {Modified: "2024-01-01"}},
		NavItems:          []page.NavItem{{Label: "Home", Path: "index.html"}},
		ProjectName:       "my-project",
		Version:           "2.5.0",
		Config:            config.Config{"name": "my-project"},
		DocsDir:           "/home/user/docs",
		OtherFiles:        []string{"logo.png"},
		HasCustomCSS:      true,
		RawThemeCSS:       "body { color: red; }",
		ThemeMeta:         &meta,
		CriticalCSS:       "h1 { font-size: 2em; }",
		ConfigDescription: "A test project",
		BaseURL:           "https://example.com",
		FeedURL:           "feed.xml",
		Lang:              "fr",
		URLBuilder:        builder,
	}

	if result.HTMLFiles["index.html"] != "<h1>Hi</h1>" {
		t.Error("HTMLFiles does not carry what it was given")
	}
	if len(result.MarkdownFiles) != 1 || result.MarkdownFiles[0].MdPath != "index.md" {
		t.Error("MarkdownFiles does not carry what it was given")
	}
	if result.Frontmatter["index.md"]["title"] != "Hi" {
		t.Error("Frontmatter does not carry what it was given")
	}
	if result.PageDates["index.md"].Modified != "2024-01-01" {
		t.Error("PageDates does not carry what it was given")
	}
	if len(result.NavItems) != 1 || result.NavItems[0].Label != "Home" {
		t.Error("NavItems does not carry what it was given")
	}
	if result.ProjectName != "my-project" || result.Version != "2.5.0" {
		t.Error("the project's name and version are not what they were given")
	}
	if result.Config["name"] != "my-project" {
		t.Error("Config does not carry what it was given")
	}
	if result.DocsDir != "/home/user/docs" {
		t.Error("DocsDir does not carry what it was given")
	}
	if len(result.OtherFiles) != 1 || result.OtherFiles[0] != "logo.png" {
		t.Error("OtherFiles does not carry what it was given")
	}
	if !result.HasCustomCSS {
		t.Error("HasCustomCSS does not carry what it was given")
	}
	if result.RawThemeCSS != "body { color: red; }" || result.CriticalCSS != "h1 { font-size: 2em; }" {
		t.Error("the stylesheets are not what they were given")
	}
	if result.ThemeMeta == nil || result.ThemeMeta.Name != "default" {
		t.Error("ThemeMeta does not carry what it was given")
	}
	if result.ConfigDescription != "A test project" {
		t.Error("ConfigDescription does not carry what it was given")
	}
	if result.BaseURL != "https://example.com" || result.FeedURL != "feed.xml" {
		t.Error("the addresses are not what they were given")
	}
	if result.Lang != "fr" {
		t.Error("Lang does not carry what it was given")
	}
	if result.URLBuilder != builder {
		t.Error("URLBuilder does not carry what it was given")
	}
}

func TestBuildResultZeroValue(t *testing.T) {
	t.Parallel()
	var result BuildResult
	if result.HTMLFiles != nil || result.MarkdownFiles != nil {
		t.Error("a zero-valued result carries pages")
	}
	// URLBuilder is nil on a result BuildSingle returned: the caller that
	// needs one builds it from the config.
	if result.URLBuilder != nil {
		t.Error("a zero-valued result carries a URL builder")
	}
}

func TestBuildResultFieldSet(t *testing.T) {
	t.Parallel()
	// The field set is pinned because every consumer reads it by name, and
	// a field added without a decision is a field nothing writes.
	want := []string{
		"HTMLFiles",
		"MarkdownFiles",
		"Frontmatter",
		"PageDates",
		"NavItems",
		"ProjectName",
		"Version",
		"Config",
		"DocsDir",
		"OtherFiles",
		"HasCustomCSS",
		"RawThemeCSS",
		"ThemeMeta",
		"CriticalCSS",
		"ConfigDescription",
		"BaseURL",
		"FeedURL",
		"Lang",
		"URLBuilder",
	}
	resultType := reflect.TypeOf(BuildResult{})
	var got []string
	for i := 0; i < resultType.NumField(); i++ {
		got = append(got, resultType.Field(i).Name)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("BuildResult declares\n  %s\nwant\n  %s",
			strings.Join(got, ", "), strings.Join(want, ", "))
	}
}
