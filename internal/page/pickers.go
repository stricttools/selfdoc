package page

import (
	"strconv"
	"strings"
	"sync"

	"github.com/stricttools/selfdoc/internal/address"
	"github.com/stricttools/selfdoc/internal/html"
	"github.com/stricttools/selfdoc/internal/urls"
)

// VersionEntry is one version the project has published, as the config
// declares it. The order of the configured list is oldest to newest: the
// version picker reads the last entry as the current one.
type VersionEntry struct {
	// Version is the version string, without a leading "v".
	Version string
}

// LocaleEntry is one locale the project publishes, as the config declares it.
type LocaleEntry struct {
	// Code is the locale's BCP 47 code, which is also its mount segment.
	Code string
	// Label is the name the locale picker shows.
	Label string
	// Default marks the locale an hreflang x-default points at.
	Default bool
}

// selectOption is one option of a picker: the value it carries, the address
// it navigates to, the text it shows (already escaped by its builder), and
// whether it is the one being rendered.
type selectOption struct {
	value    string
	href     string
	text     string
	selected bool
}

// selectCounter makes every picker's listbox id unique within a page.
//
// The framework's contract pins the SHAPE of the reference -- the button's
// aria-controls naming its own listbox -- and never the number, so a
// process-wide counter is enough and is what the Python used. The mutex is
// what keeps two concurrent page renders from minting one id twice.
var selectCounter struct {
	sync.Mutex
	next int
}

// nextSelectInstance returns the id prefix the next picker's elements carry.
func nextSelectInstance() string {
	selectCounter.Lock()
	defer selectCounter.Unlock()
	selectCounter.next++
	return "tm-sel-" + strconv.Itoa(selectCounter.next)
}

// renderSelect renders one picker in the framework's combobox shape.
//
// The markup is the APG combobox the framework's own factory builds -- a
// button[role=combobox] naming a div[role=listbox] of div[role=option] --
// emitted by the server so the control is painted and readable before any
// script runs.
//
// NO hidden native select, which the factory does emit: that element is
// legal only inside the framework's own shipped modules, and server-emitted
// markup that printed one would be a banned native control on the page.
// Nothing here submits a form, so nothing needs it.
//
// The href each option navigates to is on the option, computed by the build
// from the addressing authority -- the client does no path arithmetic.
func renderSelect(kind, label string, options []selectOption) string {
	instance := nextSelectInstance()
	current := ""
	if len(options) > 0 {
		current = options[0].text
	}
	for _, opt := range options {
		if opt.selected {
			current = opt.text
			break
		}
	}
	var opts strings.Builder
	for index, opt := range options {
		ariaSelected := "false"
		if opt.selected {
			ariaSelected = "true"
		}
		opts.WriteString(`<div class="sel-opt" id="` + instance + `-opt-` +
			strconv.Itoa(index) + `" role="option"` +
			` aria-selected="` + ariaSelected + `"` +
			` data-value="` + html.EscapeHTML(opt.value) + `"` +
			` data-href="` + html.EscapeHTML(opt.href) + `">` + opt.text + `</div>`)
	}
	return `<div class="sel ` + kind + `">` +
		`<button class="sel-btn" type="button" role="combobox"` +
		` aria-haspopup="listbox" aria-expanded="false"` +
		` aria-label="` + html.EscapeHTML(label) + `"` +
		` aria-controls="` + instance + `-listbox">` +
		`<span class="sel-label">` + current + `</span>` +
		html.ChevronIcon +
		`</button>` +
		`<div class="sel-menu" id="` + instance + `-listbox" role="listbox">` +
		opts.String() +
		`</div>` +
		`</div>` + "\n"
}

// pageHref returns the document-relative href from addr's page to another
// emitted address.
//
// Both sides come from the addressing authority: the hop out walks to the
// output root and the target's own emitted URL walks back in, so the link is
// correct under every mount point. An empty result means "this same
// directory", which is written "./" rather than "".
func pageHref(addr, target address.PageAddress) string {
	href := addr.ToSiteRoot() + target.URL()
	if href == "" {
		return "./"
	}
	return href
}

// renderVersionPicker builds the version picker for a version-scoped page.
//
// Every option carries the href the browser should go to, computed here from
// the addressing authority: the current version's option addresses the stable
// page, an older version's option addresses its archive copy. Nothing is left
// for the client to work out from location.pathname, which was only ever
// right when the site was served from an origin root.
//
// versionPages maps a version to the set of page paths it has; a version that
// does not have this page is not offered, because the link would point at a
// file no build wrote. A nil map means the caller cannot distinguish them and
// every version is offered.
func renderVersionPicker(
	addr address.PageAddress,
	availableVersions []VersionEntry,
	versionPages map[string]map[string]bool,
) (string, error) {
	if len(availableVersions) == 0 || addr.Version == "" {
		return "", nil
	}
	latestVersion := availableVersions[len(availableVersions)-1].Version
	var options []selectOption
	for _, entry := range availableVersions {
		ver := entry.Version
		if versionPages != nil && !versionPages[ver][addr.PagePath] {
			continue
		}
		target, err := address.NewPageAddress(addr.PagePath, address.Coordinates{
			Locale:   addr.Locale,
			Project:  addr.Project,
			Version:  ver,
			Archived: ver != latestVersion,
		})
		if err != nil {
			return "", err
		}
		options = append(options, selectOption{
			value:    ver,
			href:     pageHref(addr, target),
			text:     "v" + html.EscapeHTML(ver),
			selected: ver == addr.Version,
		})
	}
	if len(options) <= 1 {
		return "", nil
	}
	return renderSelect("version-picker", "Documentation version", options), nil
}

// renderLocalePicker builds the locale picker, with a server-side href per
// locale.
//
// Rendered only for a multi-locale site: with one locale there is no locale
// segment in any address and nothing for the control to switch between.
func renderLocalePicker(
	addr address.PageAddress, availableLocales []LocaleEntry, currentLocale string,
) (string, error) {
	if len(availableLocales) <= 1 {
		return "", nil
	}
	options := make([]selectOption, 0, len(availableLocales))
	for _, loc := range availableLocales {
		target, err := address.NewPageAddress(addr.PagePath, address.Coordinates{
			Locale:   loc.Code,
			Project:  addr.Project,
			Version:  addr.Version,
			Archived: addr.Archived,
		})
		if err != nil {
			return "", err
		}
		options = append(options, selectOption{
			value:    loc.Code,
			href:     pageHref(addr, target),
			text:     html.EscapeHTML(loc.Label),
			selected: loc.Code == currentLocale,
		})
	}
	return renderSelect("locale-picker", "Language", options), nil
}

// renderVersionNotice builds the "this is a superseded version" banner for an
// archive page.
//
// Rendered server-side, dismissable, and the dismissal is keyed per version:
// dismissing the notice on v0.1.0 says nothing about v0.2.0, so a reader who
// lands on a different old version is told again.
func renderVersionNotice(addr address.PageAddress) (string, error) {
	if !addr.Archived {
		return "", nil
	}
	current, err := address.NewPageAddress(addr.PagePath, address.Coordinates{
		Locale:  addr.Locale,
		Project: addr.Project,
	})
	if err != nil {
		return "", err
	}
	href := pageHref(addr, current)
	ver := html.EscapeHTML(addr.Version)
	// The framework's notice banner, in its warn kind. The dismissal split
	// is the framework's: it dresses the button and removes nothing, so the
	// click is wired by selfdoc's own version-notice script, which keys the
	// stored dismissal on data-notice-key.
	return `<div class="tm-notice tm-notice-warn" role="status"` +
		` data-notice-key="` + ver + `">` +
		`<span class="tm-notice-icon">` + html.NoticeIcon + `</span>` +
		`<div class="tm-notice-body">` +
		`<div class="tm-notice-title">Superseded version</div>` +
		`<div class="tm-notice-text">You are reading v` + ver + ` of this page, ` +
		`which has been superseded. ` +
		`<a href="` + href + `">Go to the current version</a>.</div>` +
		`</div>` +
		`<button type="button" class="tm-notice-dismiss"` +
		` aria-label="Dismiss this notice">` + html.CloseIcon + `</button>` +
		`</div>`, nil
}

// renderShareControl builds the share control for a version-scoped page.
//
// Explicit choices, never one guessed for the reader: the evergreen address,
// which always shows the current version, and -- on an archive page -- the
// pinned address, which always shows this exact version. Both are absolute --
// a shared link leaves the site -- so they come from the URL builder rather
// than from a relative hop.
//
// The pinned choice is offered only where the pinned address is a page this
// build wrote. The current version is emitted at the stable address and
// nowhere else: its "v/<version>/" address is where it WILL live once a newer
// version supersedes it, so offering it today would hand the reader a 404. A
// control that offers a dead address is worse than one that offers fewer, so
// the current version offers the evergreen address alone -- which, for it, is
// the address it is served at anyway.
func renderShareControl(
	addr address.PageAddress, ub urls.URLBuilder, baseURL string,
) string {
	if addr.Version == "" {
		return ""
	}
	absolute := func(path string) string {
		if ub != nil {
			return ub.PageURL(path)
		}
		if baseURL != "" {
			return baseURL + "/" + path
		}
		return ""
	}

	evergreen := absolute(addr.Stable)
	if evergreen == "" {
		return ""
	}
	choices := [][2]string{{evergreen, "Evergreen link (always current)"}}
	if addr.Archived {
		pinned := absolute(addr.Pinned)
		if pinned == "" {
			return ""
		}
		choices = append(choices, [2]string{
			pinned, "Pinned link (v" + html.EscapeHTML(addr.Version) + ")",
		})
	}
	var buttons strings.Builder
	for _, choice := range choices {
		buttons.WriteString(`<button type="button" class="share-address-copy"` +
			` data-share-url="` + html.EscapeHTML(choice[0]) + `">` + choice[1] +
			`</button>`)
	}
	return `<div class="share-address">` +
		`<span class="share-address-label">Share this page</span>` +
		buttons.String() +
		`</div>`
}
