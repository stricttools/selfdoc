package site

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/util"
)

// OutboundPath is the declared list of pages whose outbound links are checked.
// Absent means outbound checking is not configured, which the report says out
// loud rather than passing quietly.
const OutboundPath = "outbound.toml"

// OutboundCachePath is where the deploy keeps outbound results between runs.
const OutboundCachePath = "outbound-cache.json"

// OutboundTables is every table the outbound declaration may carry.
var OutboundTables = []string{"page"}

// OutboundKeys is every top-level key the outbound declaration may carry.
var OutboundKeys = []string{"cache_days", "page"}

// OutboundPageKeys is every key a [[page]] block may carry.
var OutboundPageKeys = []string{"path"}

// OutboundConfig is the declared outbound check: which pages, and for how
// long.
//
// Both fields are declared, neither has a default: which pages are worth the
// requests is a judgement about the site, and how long a result is trusted is
// a judgement about how fast its links rot.
type OutboundConfig struct {
	// CacheDays is how long a fetch of an outbound URL is trusted before it is
	// repeated.
	CacheDays int
	// Paths are the declared pages, relative to site/, in declared order.
	Paths []string
}

// ParseOutbound returns the outbound declaration in text, naming source in
// every diagnostic.
//
// Strict in every direction: an unknown top-level key, an unknown key on a
// [[page]] block, a missing or empty path, a repeated path and a non-positive
// cache_days are each a hard error naming the offending declaration. A file
// with no [[page]] block is a hard error too -- an empty declaration is not a
// way of saying "check nothing", it is a file somebody forgot to finish.
func ParseOutbound(text string, source string) (OutboundConfig, error) {
	if source == "" {
		source = OutboundPath
	}
	data, err := util.DecodeTOML([]byte(text))
	if err != nil {
		return OutboundConfig{}, errorf("%s is not valid TOML: %s", source, err)
	}

	if unknown := unknownKeys(data, OutboundKeys); len(unknown) > 0 {
		return OutboundConfig{}, errorf(
			"%s declares unknown key(s) %s. It carries %s and nothing else.",
			source, joinReprs(unknown), strings.Join(OutboundKeys, ", "),
		)
	}
	rawDays, present := data["cache_days"]
	if !present {
		return OutboundConfig{}, errorf(
			"%s declares no cache_days. How long an outbound result is trusted "+
				"before the link is fetched again has no default; declare it.",
			source,
		)
	}
	cacheDays, ok := rawDays.(int64)
	if !ok || cacheDays < 1 {
		return OutboundConfig{}, errorf(
			"%s: cache_days must be a whole number of days of at least 1, got %s.",
			source, util.PythonRepr(rawDays),
		)
	}

	rawValue, present := data["page"]
	raw, ok := asList(rawValue)
	if present && !ok {
		return OutboundConfig{}, errorf(
			"%s: 'page' must be a list of [[page]] blocks.", source,
		)
	}
	paths := make([]string, 0, len(raw))
	for index, itemAny := range raw {
		where := fmt.Sprintf("%s: [[page]] #%d", source, index+1)
		item, ok := asTable(itemAny)
		if !ok {
			return OutboundConfig{}, errorf("%s is not a table.", where)
		}
		if extra := unknownKeys(item, OutboundPageKeys); len(extra) > 0 {
			return OutboundConfig{}, errorf(
				"%s declares unknown key(s) %s. A [[page]] block carries %s.",
				where, joinReprs(extra), strings.Join(OutboundPageKeys, ", "),
			)
		}
		path := util.PythonStrOrEmpty(item["path"])
		if path == "" {
			return OutboundConfig{}, errorf(
				"%s declares no path. Every block names one emitted page, "+
					"relative to site/.",
				where,
			)
		}
		if containsString(paths, path) {
			return OutboundConfig{}, errorf(
				"%s repeats the path %s.", where, util.PythonRepr(path),
			)
		}
		paths = append(paths, path)
	}

	if len(paths) == 0 {
		return OutboundConfig{}, errorf(
			"%s declares no [[page]] block, so it asks for nothing. Name the "+
				"pages whose outbound links are checked, or delete the file.",
			source,
		)
	}
	return OutboundConfig{CacheDays: int(cacheDays), Paths: paths}, nil
}

// LoadOutbound returns the outbound declaration, or nil when there is no file.
//
// nil is not a default -- it is "this assembly has not configured outbound
// checking", which the report states out loud.
func LoadOutbound(assemblyDir string) (*OutboundConfig, error) {
	path := filepath.Join(assemblyDir, OutboundPath)
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	parsed, err := ParseOutbound(string(content), path)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

// LoadOutboundCache returns the outbound result store, keyed by address.
//
// An absent store is an empty one: nothing has been checked yet. A malformed
// one is a hard error -- silently starting over would refetch every link on
// every deploy and never say why.
func LoadOutboundCache(assemblyDir string) (map[string]any, error) {
	path := filepath.Join(assemblyDir, OutboundCachePath)
	entries := map[string]any{}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return entries, nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if util.PythonStrip(string(content)) == "" {
		return entries, nil
	}
	decoded, decodeErr := config.DecodeDocument(content)
	if decodeErr != nil {
		return nil, errorf("%s is not valid JSON: %v", path, decodeErr)
	}
	data, ok := asTable(decoded)
	if !ok {
		return nil, errorf("%s must contain a JSON object", path)
	}
	rawEntries := data["entries"]
	if !pythonTruthy(rawEntries) {
		return entries, nil
	}
	table, ok := asTable(rawEntries)
	if !ok {
		return nil, errorf("%s: 'entries' must be a JSON object", path)
	}
	for url, entry := range table {
		entries[url] = entry
	}
	return entries, nil
}

// RenderOutboundCache returns the JSON text of an outbound result store.
func RenderOutboundCache(entries map[string]any) (string, error) {
	encoded, err := util.PythonJSONIndent2(map[string]any{
		"schema_version": 1,
		"entries":        entries,
	})
	if err != nil {
		return "", err
	}
	return string(encoded) + "\n", nil
}
