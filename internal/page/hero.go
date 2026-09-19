package page

import (
	"strconv"
	"strings"

	"github.com/stricttools/selfdoc/internal/html"
)

// brandingString reads a string out of the branding block, answering "" for
// an absent key and for a value that is not a string.
func brandingString(branding map[string]any, key string) string {
	s, _ := branding[key].(string)
	return s
}

// generateHeroHTML builds the hero section of a landing page.
//
// branding is the project's branding block, which the caller has already
// established is present -- a project that declares none gets no hero at
// all. navItems supplies the default call-to-action target: the first page of
// the sidebar that is not the home page.
func generateHeroHTML(
	branding map[string]any, projectName, configDescription string,
	navItems []NavItem,
) string {
	var parts []string
	parts = append(parts, `<section class="hero">`)
	parts = append(parts, `<div class="hero-inner">`)

	if logo := brandingString(branding, "logo"); logo != "" {
		parts = append(parts, `<img class="hero-logo" src="`+html.EscapeHTML(logo)+
			`" alt="`+html.EscapeHTML(projectName)+` logo">`)
	}

	parts = append(parts, `<h1 class="hero-title">`+html.EscapeHTML(projectName)+`</h1>`)

	if tagline := brandingString(branding, "tagline"); tagline != "" {
		parts = append(parts, `<p class="hero-tagline">`+html.EscapeHTML(tagline)+`</p>`)
	}

	if configDescription != "" {
		parts = append(parts,
			`<p class="hero-description">`+html.EscapeHTML(configDescription)+`</p>`)
	}

	ctaLink := brandingString(branding, "cta_link")
	if ctaLink == "" {
		for _, item := range FlattenNav(navItems) {
			if item.MdPath != "index.md" {
				ctaLink = html.HTMLPathToURL(item.Path)
				break
			}
		}
	}
	if ctaLink == "" {
		ctaLink = "#"
	}

	ctaText := brandingString(branding, "cta_text")
	if ctaText == "" {
		ctaText = "Get Started"
	}

	parts = append(parts, `<div class="hero-actions">`)
	parts = append(parts, `<a class="hero-cta" href="`+html.EscapeHTML(ctaLink)+`">`+
		html.EscapeHTML(ctaText)+`</a>`)

	secondaryText := brandingString(branding, "secondary_cta_text")
	secondaryLink := brandingString(branding, "secondary_cta_link")
	if secondaryText != "" && secondaryLink != "" {
		parts = append(parts, `<a class="hero-cta hero-cta-secondary" `+
			`href="`+html.EscapeHTML(secondaryLink)+`">`+
			html.EscapeHTML(secondaryText)+`</a>`)
	}

	parts = append(parts, `</div>`)
	parts = append(parts, `</div>`)
	parts = append(parts, `</section>`)

	return strings.Join(parts, "\n")
}

// generateFeaturesHTML builds the feature grid of a landing page, or "" when
// there is nothing to show.
//
// A branding block declaring "features" states the cards itself. One that
// does not gets a card per navigation group, titled after the group and
// linking to its first page. An explicitly empty list means no grid, which is
// how a project turns the auto-generated one off.
func generateFeaturesHTML(branding map[string]any, navItems []NavItem) string {
	type featureCard struct {
		title       string
		description string
		link        string
	}
	var cards []featureCard

	declared, stated := branding["features"]
	if !stated || declared == nil {
		for _, item := range navItems {
			if !item.IsGroup() {
				continue
			}
			count := len(item.Items)
			link := ""
			if count > 0 {
				link = html.HTMLPathToURL(item.Items[0].Path)
			}
			plural := "s"
			if count == 1 {
				plural = ""
			}
			cards = append(cards, featureCard{
				title:       item.Group,
				description: strconv.Itoa(count) + " page" + plural,
				link:        link,
			})
		}
	} else {
		list, _ := declared.([]any)
		for _, raw := range list {
			feat, _ := raw.(map[string]any)
			title, _ := feat["title"].(string)
			description, _ := feat["description"].(string)
			link, _ := feat["link"].(string)
			cards = append(cards, featureCard{
				title:       title,
				description: description,
				link:        link,
			})
		}
	}

	if len(cards) == 0 {
		return ""
	}

	parts := []string{`<section class="feature-grid">`}
	for _, card := range cards {
		parts = append(parts, `<div class="feature-card">`)
		if card.link != "" {
			parts = append(parts, `<h3 class="feature-title">`+
				`<a href="`+html.EscapeHTML(card.link)+`">`+
				html.EscapeHTML(card.title)+`</a></h3>`)
		} else {
			parts = append(parts,
				`<h3 class="feature-title">`+html.EscapeHTML(card.title)+`</h3>`)
		}
		parts = append(parts,
			`<p class="feature-description">`+html.EscapeHTML(card.description)+`</p>`)
		parts = append(parts, `</div>`)
	}
	parts = append(parts, `</section>`)
	return strings.Join(parts, "\n")
}
