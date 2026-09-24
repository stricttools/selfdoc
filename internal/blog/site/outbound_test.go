package site

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stricttools/testisolation/go/hygiene"
)

// outboundDecl is a complete outbound declaration.
const outboundDecl = "cache_days = 7\n\n[[page]]\npath = \"beta/index.html\"\n"

func TestTheOutboundDeclarationRoundTrips(t *testing.T) {
	t.Parallel()
	got, err := ParseOutbound(outboundDecl, "")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := OutboundConfig{CacheDays: 7, Paths: []string{"beta/index.html"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseOutbound = %+v, want %+v", got, want)
	}
}

// The declared pages keep their declared order: the check walks them in the
// order the author wrote.
func TestOutboundPathsKeepTheirDeclaredOrder(t *testing.T) {
	t.Parallel()
	got, err := ParseOutbound(
		"cache_days = 1\n\n[[page]]\npath = \"z\"\n[[page]]\npath = \"a\"\n", "",
	)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !reflect.DeepEqual(got.Paths, []string{"z", "a"}) {
		t.Errorf("Paths = %v, want [z a]", got.Paths)
	}
}

func TestParseOutboundRefusals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		text string
		want string
	}{
		{
			name: "an unknown top-level key",
			text: "cache_days = 7\ncheck_days = 3\n\n[[page]]\npath = \"a\"\n",
			want: "outbound.toml declares unknown key(s) 'check_days'. It " +
				"carries cache_days, page and nothing else.",
		},
		{
			name: "no cache_days",
			text: "[[page]]\npath = \"a\"\n",
			want: "outbound.toml declares no cache_days. How long an outbound " +
				"result is trusted before the link is fetched again has no " +
				"default; declare it.",
		},
		{
			name: "cache_days below one",
			text: "cache_days = 0\n\n[[page]]\npath = \"a\"\n",
			want: "outbound.toml: cache_days must be a whole number of days of " +
				"at least 1, got 0.",
		},
		{
			name: "cache_days as a string",
			text: "cache_days = \"7\"\n\n[[page]]\npath = \"a\"\n",
			want: "outbound.toml: cache_days must be a whole number of days of " +
				"at least 1, got '7'.",
		},
		{
			name: "cache_days as a boolean",
			text: "cache_days = true\n\n[[page]]\npath = \"a\"\n",
			want: "outbound.toml: cache_days must be a whole number of days of " +
				"at least 1, got True.",
		},
		{
			name: "page is not a list",
			text: "cache_days = 7\npage = \"x\"\n",
			want: "outbound.toml: 'page' must be a list of [[page]] blocks.",
		},
		{
			name: "a block that is not a table",
			text: "cache_days = 7\npage = [1]\n",
			want: "outbound.toml: [[page]] #1 is not a table.",
		},
		{
			name: "an unknown key on a block",
			text: "cache_days = 7\n\n[[page]]\npath = \"a\"\nwhen = \"x\"\n",
			want: "outbound.toml: [[page]] #1 declares unknown key(s) 'when'. A " +
				"[[page]] block carries path.",
		},
		{
			name: "a block with no path",
			text: "cache_days = 7\n\n[[page]]\n",
			want: "outbound.toml: [[page]] #1 declares no path. Every block " +
				"names one emitted page, relative to site/.",
		},
		{
			name: "a repeated path",
			text: "cache_days = 7\n\n[[page]]\npath = \"a\"\n[[page]]\npath = \"a\"\n",
			want: "outbound.toml: [[page]] #2 repeats the path 'a'.",
		},
		{
			// An empty declaration is not a way of saying "check nothing", it
			// is a file somebody forgot to finish.
			name: "no page block at all",
			text: "cache_days = 7\n",
			want: "outbound.toml declares no [[page]] block, so it asks for " +
				"nothing. Name the pages whose outbound links are checked, or " +
				"delete the file.",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseOutbound(test.text, "")
			if err == nil {
				t.Fatal("want a refusal, got none")
			}
			if err.Error() != test.want {
				t.Errorf("refusal:\n%s\nwant:\n%s", err.Error(), test.want)
			}
		})
	}
}

func TestParseOutboundRefusesInvalidTOML(t *testing.T) {
	t.Parallel()
	_, err := ParseOutbound("cache_days = [7\n", "")
	if err == nil || !strings.HasPrefix(err.Error(), "outbound.toml is not valid TOML: ") {
		t.Fatalf("err = %v, want the invalid-TOML refusal", err)
	}
}

func TestParseOutboundNamesTheSourceItWasGiven(t *testing.T) {
	t.Parallel()
	_, err := ParseOutbound("cache_days = 7\n", "assembly/outbound.toml")
	if err == nil || !strings.HasPrefix(err.Error(), "assembly/outbound.toml declares no") {
		t.Fatalf("err = %v, want the source named", err)
	}
}

// nil is not a default -- it is "this assembly has not configured outbound
// checking", which the report states out loud.
func TestAnAbsentDeclarationIsNotAnEmptyOne(t *testing.T) {
	hygiene.Isolate(t)
	got, err := LoadOutbound(t.TempDir())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got != nil {
		t.Errorf("LoadOutbound = %+v, want nil", got)
	}
}

func TestLoadOutboundReadsTheDeclaration(t *testing.T) {
	hygiene.Isolate(t)
	dir := t.TempDir()
	write(t, filepath.Join(dir, OutboundPath), outboundDecl)
	got, err := LoadOutbound(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got == nil || got.CacheDays != 7 || !reflect.DeepEqual(got.Paths, []string{"beta/index.html"}) {
		t.Errorf("LoadOutbound = %+v", got)
	}
}

func TestLoadOutboundNamesTheFileInItsRefusals(t *testing.T) {
	hygiene.Isolate(t)
	dir := t.TempDir()
	path := filepath.Join(dir, OutboundPath)
	write(t, path, "cache_days = 7\n")
	_, err := LoadOutbound(dir)
	if err == nil || !strings.HasPrefix(err.Error(), path+" declares no") {
		t.Fatalf("err = %v, want the refusal to name %s", err, path)
	}
}

// -- the result store --------------------------------------------------------

func TestAnAbsentCacheIsAnEmptyOne(t *testing.T) {
	hygiene.Isolate(t)
	entries, err := LoadOutboundCache(t.TempDir())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("entries = %v, want empty", entries)
	}
}

func TestAnEmptyCacheFileIsAnEmptyStore(t *testing.T) {
	hygiene.Isolate(t)
	dir := t.TempDir()
	write(t, filepath.Join(dir, OutboundCachePath), "  \n")
	entries, err := LoadOutboundCache(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("entries = %v, want empty", entries)
	}
}

// A malformed store is a hard error -- silently starting over would refetch
// every link on every deploy and never say why.
func TestACorruptCacheIsAHardError(t *testing.T) {
	hygiene.Isolate(t)
	dir := t.TempDir()
	path := filepath.Join(dir, OutboundCachePath)
	write(t, path, "{not json")
	_, err := LoadOutboundCache(dir)
	if err == nil || !strings.HasPrefix(err.Error(), path+" is not valid JSON: ") {
		t.Fatalf("err = %v, want the invalid-JSON refusal", err)
	}
}

func TestACacheThatIsNotAnObjectIsAHardError(t *testing.T) {
	hygiene.Isolate(t)
	dir := t.TempDir()
	path := filepath.Join(dir, OutboundCachePath)
	write(t, path, "[]")
	_, err := LoadOutboundCache(dir)
	if err == nil || err.Error() != path+" must contain a JSON object" {
		t.Fatalf("err = %v, want the non-object refusal", err)
	}
}

func TestCacheEntriesThatAreNotAnObjectIsAHardError(t *testing.T) {
	hygiene.Isolate(t)
	dir := t.TempDir()
	path := filepath.Join(dir, OutboundCachePath)
	write(t, path, `{"schema_version": 1, "entries": [1]}`)
	_, err := LoadOutboundCache(dir)
	if err == nil || err.Error() != path+": 'entries' must be a JSON object" {
		t.Fatalf("err = %v, want the entries refusal", err)
	}
}

func TestRenderOutboundCacheIsByteIdenticalToThePython(t *testing.T) {
	t.Parallel()
	got, err := RenderOutboundCache(map[string]any{
		"https://b.example/": map[string]any{
			"status": 200, "checked": 5.0, "error": "",
		},
		"https://a.example/": map[string]any{
			"status": 404, "checked": 1.5, "error": "404",
		},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if want := reference(t, "outbound-cache.json"); got != want {
		t.Errorf("rendered store:\n%q\nwant:\n%q", got, want)
	}
}

func TestARenderedCacheRoundTrips(t *testing.T) {
	hygiene.Isolate(t)
	dir := t.TempDir()
	rendered, err := RenderOutboundCache(map[string]any{
		"https://a.example/": map[string]any{"status": 200},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	write(t, filepath.Join(dir, OutboundCachePath), rendered)
	entries, err := LoadOutboundCache(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	entry, ok := entries["https://a.example/"].(map[string]any)
	if !ok {
		t.Fatalf("entries = %v", entries)
	}
	if entry["status"] != int64(200) {
		t.Errorf("status = %#v, want 200", entry["status"])
	}
}
