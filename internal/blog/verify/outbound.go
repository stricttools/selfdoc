package verify

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/stricttools/selfdoc/internal/blog/site"
	"github.com/stricttools/selfdoc/internal/resolution"
)

// secondsPerDay converts the declared cache window, which is a whole number of
// days, into the seconds the stored timestamps are measured in.
const secondsPerDay = 86400

// outboundTimeout is how long one outbound fetch is given to answer.
const outboundTimeout = 15 * time.Second

// outboundUserAgent identifies the verification to the servers it asks.
const outboundUserAgent = "selfdoc-assembly-verify"

// Fetcher fetches a URL and returns its status and the error text, which is
// empty when there was none.
//
// It is the one seam in this package that leaves the machine, and the one a
// test replaces. A failed transport is status 0 with the reason, so a caller
// never has to tell "did not answer" from "answered badly" by inspecting an
// error value.
type Fetcher func(url string) (int, string)

// FetchURL is the default Fetcher: a GET with a timeout, mapping every
// transport failure to status 0 and the failure's text.
//
// It is a GET: it changes nothing, which is why it does not go through the
// effects handle.
func FetchURL(url string) (int, string) {
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return 0, err.Error()
	}
	request.Header.Set("User-Agent", outboundUserAgent)
	client := &http.Client{Timeout: outboundTimeout}
	response, err := client.Do(request)
	if err != nil {
		return 0, err.Error()
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		// The reason phrase alone, without the numeric code Go prefixes it
		// with: the code is the other half of the pair already.
		reason := strings.TrimSpace(strings.TrimPrefix(
			response.Status, strconv.Itoa(response.StatusCode),
		))
		return response.StatusCode, reason
	}
	return response.StatusCode, ""
}

// Now is the wall clock in the units the outbound store records, for a caller
// that is verifying a real tree rather than pinning an instant in a test.
//
// It exists so the one conversion from a Go clock to the store's seconds lives
// in one place: the stored timestamps came from Python's time.time() and are
// compared against the declared cache window in seconds.
func Now() float64 {
	return float64(time.Now().UnixNano()) / float64(time.Second)
}

// CheckOutboundLinks fetches the outbound links on the declared pages and
// returns the failures, the store as this run leaves it, and how many requests
// it made.
//
// A cached result inside the window answers without a request, so a second run
// over an unchanged tree makes none at all.
func CheckOutboundLinks(
	tree *AssemblyTree,
	config site.OutboundConfig,
	cache map[string]any,
	fetch Fetcher,
	now float64,
) ([]Failure, map[string]any, int, error) {
	var failures []Failure
	updated := map[string]any{}
	for url, entry := range cache {
		updated[url] = entry
	}
	window := float64(config.CacheDays) * secondsPerDay
	requests := 0

	for _, rel := range config.Paths {
		if !tree.Emitted[rel] {
			failures = append(failures, Failure{
				"outbound-links", "site/" + rel,
				"is declared in outbound.toml but was not emitted, so its " +
					"outbound links are never checked.",
			})
			continue
		}
		pageHTML, err := tree.Read(rel)
		if err != nil {
			return nil, nil, 0, err
		}
		for _, url := range distinct(resolution.ExternalReferences(pageHTML)) {
			if _, ok := resolution.SiteRelativePath(url, tree.CanonicalBase); ok {
				continue
			}
			entry, _ := updated[url].(map[string]any)
			fresh := false
			if entry != nil {
				if checked, ok := asSeconds(entry["checked"]); ok && now-checked < window {
					fresh = true
				}
			}
			if !fresh {
				status, errorText := fetch(url)
				requests++
				entry = map[string]any{
					"checked": now,
					"status":  status,
					"ok":      status >= 200 && status < 400,
					"error":   errorText,
				}
				updated[url] = entry
			}
			if ok, _ := entry["ok"].(bool); !ok {
				detail, _ := entry["error"].(string)
				status := "nothing"
				if code, found := asSeconds(entry["status"]); found && code != 0 {
					status = fmt.Sprintf("%d", int64(code))
				}
				suffix := ""
				if detail != "" {
					suffix = ": " + detail
				}
				failures = append(failures, Failure{
					"outbound-links", "site/" + rel,
					fmt.Sprintf("links to %s, which answered %s%s.",
						url, status, suffix),
				})
			}
		}
	}
	return failures, updated, requests, nil
}

// asSeconds reads a stored number as a float, accepting both shapes a decoded
// store can carry -- the integer a hand-written entry declares and the float
// this package writes.
func asSeconds(value any) (float64, bool) {
	switch number := value.(type) {
	case float64:
		return number, true
	case int64:
		return float64(number), true
	case int:
		return float64(number), true
	default:
		return 0, false
	}
}

// distinct returns items with later repeats dropped, preserving first-seen
// order.
func distinct(items []string) []string {
	seen := map[string]bool{}
	var kept []string
	for _, item := range items {
		if seen[item] {
			continue
		}
		seen[item] = true
		kept = append(kept, item)
	}
	return kept
}
