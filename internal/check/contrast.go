package check

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/stricttools/selfdoc/internal/lints"
	"github.com/stricttools/selfdoc/internal/themes"
)

// rgb is a colour parsed out of a stylesheet, each channel 0-255.
type rgb struct{ R, G, B int }

// parseHexColor parses a "#RRGGBB" hex colour, reporting whether it is one.
func parseHexColor(hexColor string) (rgb, bool) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(hexColor), "#")
	if len(trimmed) != 6 {
		return rgb{}, false
	}
	channels := [3]int{}
	for i := 0; i < 3; i++ {
		value, err := strconv.ParseUint(trimmed[i*2:i*2+2], 16, 16)
		if err != nil {
			return rgb{}, false
		}
		channels[i] = int(value)
	}
	return rgb{channels[0], channels[1], channels[2]}, true
}

// relativeLuminance computes the WCAG 2.1 relative luminance of a colour.
func relativeLuminance(color rgb) float64 {
	linear := func(channel int) float64 {
		srgb := float64(channel) / 255.0
		if srgb <= 0.04045 {
			return srgb / 12.92
		}
		return math.Pow((srgb+0.055)/1.055, 2.4)
	}
	return 0.2126*linear(color.R) + 0.7152*linear(color.G) + 0.0722*linear(color.B)
}

// contrastRatio computes the WCAG 2.1 contrast ratio between two colours.
func contrastRatio(first, second rgb) float64 {
	a := relativeLuminance(first)
	b := relativeLuminance(second)
	lighter, darker := math.Max(a, b), math.Min(a, b)
	return (lighter + 0.05) / (darker + 0.05)
}

// cssVarPattern matches one CSS custom-property declaration.
var cssVarPattern = regexp.MustCompile(`(--[\p{L}\p{N}_-]+)\s*:\s*([^;]+);`)

// extractCSSVars extracts the CSS custom properties declared in a block of
// stylesheet text, keyed by property name ("--bg").
func extractCSSVars(cssBlock string) map[string]string {
	props := map[string]string{}
	for _, match := range cssVarPattern.FindAllStringSubmatch(cssBlock, -1) {
		props[match[1]] = strings.TrimSpace(match[2])
	}
	return props
}

// rootBlockPattern matches the light-mode custom-property block.
var rootBlockPattern = regexp.MustCompile(`:root\s*\{([^}]+)\}`)

// darkBlockPattern matches the dark-mode custom-property block.
var darkBlockPattern = regexp.MustCompile(`\[data-theme="dark"\]\s*\{([^}]+)\}`)

// contrastPair is one foreground/background pair SEO012 measures.
type contrastPair struct {
	// Foreground and Background are the custom properties the pair reads.
	Foreground, Background string
	// Label names the pair in the diagnostic.
	Label string
	// Threshold is the ratio WCAG AA requires of it.
	Threshold float64
}

// contrastPairs are the critical pairs a theme is measured on.
var contrastPairs = []contrastPair{
	{"--text", "--bg", "body text", 4.5},
	{"--text-secondary", "--bg", "secondary text", 4.5},
	{"--heading", "--bg", "headings", 3.0},
	{"--link", "--bg", "links", 4.5},
	{"--sidebar-text", "--sidebar-bg", "sidebar text", 4.5},
}

// ThemeCSS returns the stylesheet SEO012 measures for a theme.
//
// The Python resolved a path on disk because the themes shipped as files in an
// installed package; here they are embedded, so the bytes come out of the
// theme registry instead. It is the theme's OWN sheet -- what
// themes.Overlay answers -- because that is the file that declares the
// custom properties a page is painted with; a framework theme's framework
// sheets carry the framework's own palette and are not selfdoc's to correct.
//
// The second result is false for a theme this build does not ship, which is
// the counterpart of the Python's absent file: there is nothing to measure and
// no diagnostic to make.
func ThemeCSS(themeName string) (string, bool) {
	css, err := themes.Overlay(themeName)
	if err != nil {
		return "", false
	}
	return css, true
}

// checkContrast appends an SEO012 diagnostic for every theme colour pair whose
// contrast ratio is under its WCAG 2.1 threshold.
//
// The theme's own stylesheet is measured first, in both colour schemes, and
// then the project's docs/custom.css overrides merged over it -- an override
// that darkens a link past the threshold is the project's defect, reported
// against the file that declares it.
func checkContrast(results []lints.LintResult, config map[string]any, docsDir string) []lints.LintResult {
	themeName := configString(config, "theme", "minimal")

	cssContent, present := ThemeCSS(themeName)
	if !present {
		return results
	}

	var lightVars, darkVars map[string]string
	if match := rootBlockPattern.FindStringSubmatch(cssContent); match != nil {
		lightVars = extractCSSVars(match[1])
	}
	if match := darkBlockPattern.FindStringSubmatch(cssContent); match != nil {
		darkVars = extractCSSVars(match[1])
	}

	if len(lightVars) > 0 {
		results = checkPairs(results, lightVars, "", "theme CSS")
	}
	if len(darkVars) > 0 {
		results = checkPairs(results, darkVars, "dark mode ", "theme CSS")
	}

	customCSSPath := filepath.Join(docsDir, "custom.css")
	if !isFile(customCSSPath) {
		return results
	}
	raw, err := os.ReadFile(customCSSPath)
	if err != nil {
		return results
	}
	customContent := string(raw)

	if match := rootBlockPattern.FindStringSubmatch(customContent); match != nil && len(lightVars) > 0 {
		results = checkPairs(
			results, mergeVars(lightVars, extractCSSVars(match[1])),
			"", "custom.css",
		)
	}
	if match := darkBlockPattern.FindStringSubmatch(customContent); match != nil && len(darkVars) > 0 {
		results = checkPairs(
			results, mergeVars(darkVars, extractCSSVars(match[1])),
			"dark mode ", "custom.css",
		)
	}
	return results
}

// mergeVars layers overrides over base, leaving both untouched.
func mergeVars(base, overrides map[string]string) map[string]string {
	merged := make(map[string]string, len(base)+len(overrides))
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range overrides {
		merged[key] = value
	}
	return merged
}

// checkPairs measures each pair against cssVars and appends an SEO012
// diagnostic for every one below its threshold.
func checkPairs(
	results []lints.LintResult,
	cssVars map[string]string,
	modePrefix, cssFile string,
) []lints.LintResult {
	for _, pair := range contrastPairs {
		foregroundHex := cssVars[pair.Foreground]
		backgroundHex := cssVars[pair.Background]
		if foregroundHex == "" || backgroundHex == "" {
			continue
		}
		foreground, okForeground := parseHexColor(foregroundHex)
		background, okBackground := parseHexColor(backgroundHex)
		if !okForeground || !okBackground {
			continue
		}
		ratio := contrastRatio(foreground, background)
		if ratio >= pair.Threshold {
			continue
		}
		results = append(results, lints.MustLintResult(
			cssFile, nil, "SEO012",
			fmt.Sprintf(
				"Low contrast ratio %.1f:1 for %s%s on %s (WCAG AA requires %s:1)",
				ratio, modePrefix, pair.Label, pair.Background,
				formatThreshold(pair.Threshold),
			),
		))
	}
	return results
}

// formatThreshold renders a threshold the way Python's str() of the float in
// the message does: "4.5", and "3.0" rather than "3".
func formatThreshold(threshold float64) string {
	return strconv.FormatFloat(threshold, 'f', -1, 64) + thresholdFraction(threshold)
}

// thresholdFraction supplies the ".0" Python's repr of an integral float
// carries and Go's shortest formatting drops.
func thresholdFraction(threshold float64) string {
	if threshold == math.Trunc(threshold) {
		return ".0"
	}
	return ""
}
