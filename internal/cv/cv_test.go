// The CV document: strict parsing, one rendering, and the Person it states.
//
// A CV is a record, so it is declared as data and rendered from there. These
// tests hold the two ends together: every fact the document declares reaches
// the page, and the Person the page emits carries what a CV knows on top of
// the site's declared author.
package cv

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/identity"
	"github.com/stricttools/testisolation/go/hygiene"
)

// testAuthor is the author a site declares, as the shared test fixture
// declares it.
func testAuthor() map[string]any {
	return map[string]any{
		"name":    "Test Author",
		"url":     "https://author.example",
		"same_as": []any{"https://github.com/testauthor"},
	}
}

const document = `
format_version = 1

[identity]
name = "Ada Lovelace"
headline = "Analyst"
location = "London, England"
email = "ada@example.org"
photo = "pic.jpg"
updated = "October 10th, 1852"
summary = "I write [notes](https://example.org/notes) about engines."

  [[identity.profile]]
  label = "example.org/ada"
  url = "https://example.org/ada"

[[skills]]
category = "Languages"
items = ["Analytical Engine notation", "French"]

[[skills]]
category = "Other"
items = ["Correspondence"]

[[projects]]
name = "Note G"
notes = ["The first published algorithm"]
technologies = ["Analytical Engine"]

[[projects]]
name = "Translation"
technologies = ["French"]

[[interests]]
title = "Poetical science"
body = "Imagination is the *discovering* faculty."

[[education]]
degree = "Private tuition in mathematics"
years = "1833 - 1840"
institute = "University of London"
institute_url = "https://london.example"
location = "London, England"
focus = "Mathematics"
thesis = "On the Analytical Engine"
course_url = "https://london.example/course"

[[experience]]
role = "Translator and analyst"
period = "1842 - 1843"
company = "Scientific Memoirs"
company_url = "https://memoirs.example"
location = "London, England"
body = "Translated Menabrea's memoir and appended notes three times its length."

[[experience]]
role = "Correspondent"
period = "1840"
company = "Self-employed"
location = "England"

[[languages]]
name = "English"
level = "Native"

[[languages]]
name = "French"
url = "https://example.org/french"
level = "Fluent"

[contact]
body = "Write to [ada@example.org](mailto:ada@example.org)."
`

// without returns the document with one whole section removed.
func without(blockKey string) string {
	var out []string
	skipping := false
	for _, line := range strings.Split(document, "\n") {
		if strings.HasPrefix(line, "[[") || (strings.HasPrefix(line, "[") && !strings.HasPrefix(line, "[[")) {
			skipping = strings.HasPrefix(line, "[["+blockKey+"]]") || line == "["+blockKey+"]"
		}
		if !skipping {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// parse parses the document and fails the test when it will not.
func parse(t *testing.T, text string) *CV {
	t.Helper()
	cv, err := ParseCV(text, CVSource)
	if err != nil {
		t.Fatalf("ParseCV: %v", err)
	}
	return cv
}

// refuses asserts that parsing text fails with a message carrying phrase.
func refuses(t *testing.T, text, phrase string) {
	t.Helper()
	_, err := ParseCV(text, CVSource)
	if err == nil {
		t.Fatalf("the document was accepted; expected a refusal naming %q", phrase)
	}
	if !strings.Contains(err.Error(), phrase) {
		t.Errorf("refusal %q does not name %q", err, phrase)
	}
}

// -- parsing ----------------------------------------------------------------

func TestTheDocumentParses(t *testing.T) {
	cv := parse(t, document)
	if cv.Identity.Name != "Ada Lovelace" {
		t.Errorf("name is %q", cv.Identity.Name)
	}
	var categories []string
	for _, group := range cv.Skills {
		categories = append(categories, group.Category)
	}
	if strings.Join(categories, ",") != "Languages,Other" {
		t.Errorf("skill categories are %v", categories)
	}
	var projects []string
	for _, project := range cv.Projects {
		projects = append(projects, project.Name)
	}
	if strings.Join(projects, ",") != "Note G,Translation" {
		t.Errorf("projects are %v", projects)
	}
	if len(cv.Education) != 1 {
		t.Errorf("education holds %d entries", len(cv.Education))
	}
	if len(cv.Experience) != 2 {
		t.Errorf("experience holds %d entries", len(cv.Experience))
	}
	var languages []string
	for _, language := range cv.Languages {
		languages = append(languages, language.Name)
	}
	if strings.Join(languages, ",") != "English,French" {
		t.Errorf("languages are %v", languages)
	}
	if !strings.HasPrefix(cv.Contact, "Write to") {
		t.Errorf("contact is %q", cv.Contact)
	}
}

func TestAWrongFormatVersionIsRefused(t *testing.T) {
	refuses(t, strings.Replace(document, "format_version = 1", "format_version = 2", 1), "format_version")
}

func TestAnUnknownTopLevelKeyIsRefused(t *testing.T) {
	refuses(t, document+"\n[publications]\nbody = \"none\"\n", "publications")
}

func TestAnUnknownIdentityKeyIsRefused(t *testing.T) {
	refuses(t, strings.Replace(
		document, `name = "Ada Lovelace"`, "name = \"Ada\"\nnickname = \"AAL\"", 1,
	), "nickname")
}

func TestAnUnknownBlockKeyIsRefused(t *testing.T) {
	refuses(t, strings.Replace(
		document, `focus = "Mathematics"`, "focus = \"Mathematics\"\ngrade = \"110\"", 1,
	), "grade")
}

func TestAnAbsentSectionIsRefused(t *testing.T) {
	for _, section := range []string{
		"skills", "projects", "interests", "education", "experience", "languages",
	} {
		refuses(t, without(section), section)
	}
}

func TestAnAbsentIdentityIsRefused(t *testing.T) {
	refuses(t, without("identity"), "identity")
}

func TestAnAbsentContactIsRefused(t *testing.T) {
	refuses(t, without("contact"), "contact")
}

func TestAMissingRequiredFieldIsRefused(t *testing.T) {
	refuses(t, strings.Replace(document, "email = \"ada@example.org\"\n", "", 1), "'email'")
}

func TestAnEmptyRequiredFieldIsRefused(t *testing.T) {
	refuses(t, strings.Replace(
		document, `headline = "Analyst"`, `headline = "  "`, 1,
	), "'headline'")
}

func TestARepeatedSkillCategoryIsRefused(t *testing.T) {
	refuses(t, strings.Replace(
		document, `category = "Other"`, `category = "Languages"`, 1,
	), "repeats")
}

func TestARepeatedProjectIsRefused(t *testing.T) {
	refuses(t, strings.Replace(
		document, `name = "Translation"`, `name = "Note G"`, 1,
	), "repeats")
}

func TestARepeatedInterestIsRefused(t *testing.T) {
	refuses(t, document+
		"\n[[interests]]\ntitle = \"Poetical science\"\nbody = \"Said twice.\"\n",
		"Poetical science")
}

func TestARepeatedEducationEntryIsRefused(t *testing.T) {
	// Same degree at the same school, twice -- one of them is a mistake.
	//
	// The pair is what identifies the entry: two different degrees from one
	// university, or one degree from two universities, are both ordinary.
	refuses(t, document+
		"\n[[education]]\n"+
		"degree = \"Private tuition in mathematics\"\n"+
		"years = \"1841 - 1842\"\n"+
		"institute = \"University of London\"\n"+
		"location = \"London, England\"\n",
		"University of London")
}

func TestTwoDegreesFromOneInstituteAreFine(t *testing.T) {
	cv := parse(t, document+
		"\n[[education]]\n"+
		"degree = \"Advanced tuition in mathematics\"\n"+
		"years = \"1841 - 1842\"\n"+
		"institute = \"University of London\"\n"+
		"location = \"London, England\"\n")
	if len(cv.Education) != 2 {
		t.Errorf("education holds %d entries", len(cv.Education))
	}
}

func TestARepeatedExperienceEntryIsRefused(t *testing.T) {
	// Role, employer and period together identify a post.
	refuses(t, document+
		"\n[[experience]]\nrole = \"Correspondent\"\n"+
		"period = \"1840\"\ncompany = \"Self-employed\"\n"+
		"location = \"England\"\n",
		"Correspondent")
}

func TestTwoStintsInOneRoleAtOneCompanyAreFine(t *testing.T) {
	cv := parse(t, document+
		"\n[[experience]]\nrole = \"Correspondent\"\n"+
		"period = \"1845\"\ncompany = \"Self-employed\"\n"+
		"location = \"England\"\n")
	if len(cv.Experience) != 3 {
		t.Errorf("experience holds %d entries", len(cv.Experience))
	}
}

func TestARepeatedLanguageIsRefused(t *testing.T) {
	refuses(t, strings.Replace(document, `name = "French"`, `name = "English"`, 1), "English")
}

func TestAProjectWithNothingToSayIsRefused(t *testing.T) {
	refuses(t, strings.Replace(
		document,
		"name = \"Translation\"\ntechnologies = [\"French\"]",
		`name = "Translation"`, 1,
	), "bare heading")
}

func TestMalformedTOMLNamesTheSource(t *testing.T) {
	_, err := ParseCV("format_version = ", "cv.toml")
	if err == nil {
		t.Fatal("a syntax error was accepted")
	}
	if !strings.Contains(err.Error(), "cv.toml is not valid TOML") {
		t.Errorf("refusal is %q", err)
	}
}

func TestAnEmptySectionListIsRefused(t *testing.T) {
	// A declared-but-empty section renders as a heading over nothing, which is
	// the very thing the non-empty rule exists to prevent. The declaration
	// goes before the first table, so it is the top-level key and not one the
	// last table would swallow.
	refuses(t, "languages = []\n"+without("languages"),
		"declares no [[languages]] block")
}

func TestLoadCVReadsTheDocumentFromDisk(t *testing.T) {
	hygiene.Isolate(t)
	path := filepath.Join(t.TempDir(), "cv.toml")
	if err := os.WriteFile(path, []byte(document), 0o644); err != nil {
		t.Fatal(err)
	}
	cv, err := LoadCV(path)
	if err != nil {
		t.Fatalf("LoadCV: %v", err)
	}
	if cv.Identity.Name != "Ada Lovelace" {
		t.Errorf("name is %q", cv.Identity.Name)
	}
}

func TestLoadCVNamesThePathInItsRefusals(t *testing.T) {
	hygiene.Isolate(t)
	path := filepath.Join(t.TempDir(), "broken.toml")
	if err := os.WriteFile(path, []byte("format_version = "), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadCV(path)
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Errorf("refusal is %v, and does not name %q", err, path)
	}
}

// -- rendering --------------------------------------------------------------

func TestEveryDeclaredFactReachesThePage(t *testing.T) {
	page := RenderCVMarkdown(parse(t, document))
	for _, fragment := range []string{
		">Ada Lovelace</h2>",
		`src="pic.jpg"`,
		"Analyst",
		"London, England",
		`<a href="mailto:ada@example.org">ada@example.org</a>`,
		`<a href="https://example.org/ada">example.org/ada</a>`,
		"I write [notes](https://example.org/notes) about engines.",

		"- **Languages:** Analytical Engine notation, French",
		"- **Other:** Correspondence",

		"### Note G",
		"- The first published algorithm",
		"- **Technologies used:** Analytical Engine",
		"### Translation",

		"### Poetical science",
		"Imagination is the *discovering* faculty.",

		"### Private tuition in mathematics",
		"- **Year:** 1833 - 1840",
		"- **Institute:** [University of London](https://london.example)",
		"- **Focus:** Mathematics",
		"- **Thesis:** On the Analytical Engine",
		"- [Course details](https://london.example/course)",

		"### Translator and analyst",
		"- **Period:** 1842 - 1843",
		"- **Company:** [Scientific Memoirs](https://memoirs.example)",
		"Translated Menabrea's memoir",
		"### Correspondent",
		"- **Company:** Self-employed",

		"- English – Native",
		"- [French](https://example.org/french) – Fluent",

		"Write to [ada@example.org](mailto:ada@example.org).",
		"Last updated on October 10th, 1852",
	} {
		if !strings.Contains(page, fragment) {
			t.Errorf("the page does not carry %q", fragment)
		}
	}
}

func TestTheSectionHeadingsAreTheDocumentsStructure(t *testing.T) {
	page := RenderCVMarkdown(parse(t, document))
	for _, heading := range []string{
		"## Skills", "## Projects", "## Hobbies & interests", "## Education",
		"## Work experience", "## Languages", "## Contact information",
	} {
		if !strings.Contains(page, heading) {
			t.Errorf("the page does not carry %q", heading)
		}
	}
}

func TestAnAbsentPhotoEmitsNoImage(t *testing.T) {
	page := RenderCVMarkdown(parse(t, strings.Replace(document, "photo = \"pic.jpg\"\n", "", 1)))
	if strings.Contains(page, "cv-photo") || strings.Contains(page, "<img") {
		t.Errorf("the page carries an image:\n%s", page)
	}
}

func TestThePageEndsWithExactlyOneNewline(t *testing.T) {
	// The body is joined and then trimmed, so a section that emitted a
	// trailing blank line cannot add a second one.
	page := RenderCVMarkdown(parse(t, document))
	if !strings.HasSuffix(page, "</div>\n") || strings.HasSuffix(page, "\n\n") {
		t.Errorf("the page ends %q", page[len(page)-20:])
	}
}

// -- the header is structure ------------------------------------------------

// The CV opens with the person, not with a paragraph of loose facts.
//
// Flattening the header to Markdown produced a bare image on one line and the
// headline, location, email and profile links running together on the next,
// with nothing for a theme to style. The block is emitted as real markup
// instead, so the design is carried by the themes.

func TestTheHeaderIsOneBlock(t *testing.T) {
	page := RenderCVMarkdown(parse(t, document))
	if strings.Count(page, `<div class="cv-header">`) != 1 {
		t.Errorf("the page carries %d header blocks", strings.Count(page, `<div class="cv-header">`))
	}
	// One line, so the Markdown converter passes it through whole.
	var headerLine string
	for _, line := range strings.Split(page, "\n") {
		if strings.Contains(line, "cv-header") {
			headerLine = line
			break
		}
	}
	if headerLine == "" {
		t.Fatal("no line carries the header")
	}
	if !strings.HasSuffix(headerLine, "</div>") {
		t.Errorf("the header line ends %q", headerLine)
	}
}

func TestTheHeaderCarriesThePortrait(t *testing.T) {
	page := RenderCVMarkdown(parse(t, document))
	want := `<div class="cv-photo"><img src="pic.jpg" alt="Profile picture"></div>`
	if !strings.Contains(page, want) {
		t.Errorf("the page does not carry %q", want)
	}
}

func TestTheHeaderNamesThePerson(t *testing.T) {
	page := RenderCVMarkdown(parse(t, document))
	if !strings.Contains(page, `<h2 class="cv-name">Ada Lovelace</h2>`) {
		t.Error("the header does not name the person in an h2")
	}
}

func TestTheDetailsAreSeparableItems(t *testing.T) {
	// Each fact is its own span, so the theme can divide them.
	page := RenderCVMarkdown(parse(t, document))
	pattern := regexp.MustCompile(`(?s)<div class="cv-subtitle">(.*?)</div>`)
	match := pattern.FindStringSubmatch(page)
	if match == nil {
		t.Fatalf("no subtitle block:\n%s", page)
	}
	if got := strings.Count(match[1], "<span>"); got != 4 {
		t.Errorf("the subtitle carries %d spans", got)
	}
	for _, fragment := range []string{
		"<span>Analyst</span>", "<span>London, England</span>",
	} {
		if !strings.Contains(match[1], fragment) {
			t.Errorf("the subtitle does not carry %q", fragment)
		}
	}
}

func TestTheClosingDateIsItsOwnElement(t *testing.T) {
	page := RenderCVMarkdown(parse(t, document))
	want := `<div class="cv-updated">Last updated on October 10th, 1852</div>`
	if !strings.Contains(page, want) {
		t.Errorf("the page does not carry %q", want)
	}
}

func TestHTMLSpecialCharactersAreEscaped(t *testing.T) {
	cv := parse(t, strings.Replace(
		document, `name = "Ada Lovelace"`, `name = "Ada <b>Lovelace</b> & Co"`, 1,
	))
	page := RenderCVMarkdown(cv)
	if !strings.Contains(page, "Ada &lt;b&gt;Lovelace&lt;/b&gt; &amp; Co") {
		t.Errorf("the name is not escaped:\n%s", page)
	}
}

func TestTheBodyStaysMarkdown(t *testing.T) {
	// Only the header and the closing date are markup.
	//
	// Sections stay Markdown headings so they keep their anchors and reach the
	// table of contents.
	page := RenderCVMarkdown(parse(t, document))
	if !strings.Contains(page, "## Skills") {
		t.Error("the sections are not Markdown headings")
	}
	if strings.Contains(page, `<h2 class="cv-section"`) {
		t.Error("a section heading was emitted as markup")
	}
}

// -- the Person a CV states -------------------------------------------------

// property returns an entity's value for key and fails when it carries none.
func property(t *testing.T, entity identity.Entity, key string) any {
	t.Helper()
	value, ok := entity.Get(key)
	if !ok {
		t.Fatalf("the Person carries no %q: %v", key, entity.Keys())
	}
	return value
}

// person builds the Person the document states against the given author.
func person(t *testing.T, author map[string]any) identity.Entity {
	t.Helper()
	entity, err := CVPersonJSONLD(parse(t, document), author)
	if err != nil {
		t.Fatalf("CVPersonJSONLD: %v", err)
	}
	return entity
}

func TestTheIdentityComesFromTheDeclaredAuthor(t *testing.T) {
	entity := person(t, testAuthor())
	if got := property(t, entity, "@type"); got != "Person" {
		t.Errorf("@type is %v", got)
	}
	if got := property(t, entity, "name"); got != "Test Author" {
		t.Errorf("name is %v", got)
	}
	if got := property(t, entity, "url"); got != "https://author.example" {
		t.Errorf("url is %v", got)
	}
}

func TestAFullerSpellingOfTheNameRidesAsAnAlternate(t *testing.T) {
	if got := property(t, person(t, testAuthor()), "alternateName"); got != "Ada Lovelace" {
		t.Errorf("alternateName is %v", got)
	}
}

func TestTheSameNameIsNotRepeatedAsAnAlternate(t *testing.T) {
	author := testAuthor()
	author["name"] = "Ada Lovelace"
	if _, ok := person(t, author).Get("alternateName"); ok {
		t.Error("the Person repeats its own name as an alternate")
	}
}

func TestTheCVContributesWhatACVKnows(t *testing.T) {
	entity := person(t, testAuthor())
	if got := property(t, entity, "jobTitle"); got != "Analyst" {
		t.Errorf("jobTitle is %v", got)
	}
	if got := property(t, entity, "email"); got != "mailto:ada@example.org" {
		t.Errorf("email is %v", got)
	}
	address, ok := property(t, entity, "address").(identity.Entity)
	if !ok {
		t.Fatalf("address is %T", property(t, entity, "address"))
	}
	if got, _ := address.Get("addressLocality"); got != "London, England" {
		t.Errorf("addressLocality is %v", got)
	}
	spoken, ok := property(t, entity, "knowsLanguage").([]any)
	if !ok {
		t.Fatalf("knowsLanguage is %T", property(t, entity, "knowsLanguage"))
	}
	var names []string
	for _, item := range spoken {
		language, isEntity := item.(identity.Entity)
		if !isEntity {
			t.Fatalf("a language is %T", item)
		}
		name, _ := language.Get("name")
		names = append(names, name.(string))
	}
	if strings.Join(names, ",") != "English,French" {
		t.Errorf("knowsLanguage names are %v", names)
	}
	schools, ok := property(t, entity, "alumniOf").([]any)
	if !ok || len(schools) != 1 {
		t.Fatalf("alumniOf is %v", property(t, entity, "alumniOf"))
	}
	school := schools[0].(identity.Entity)
	if strings.Join(school.Keys(), ",") != "@type,name,url" {
		t.Errorf("the school carries %v", school.Keys())
	}
	for key, want := range map[string]string{
		"@type": "EducationalOrganization",
		"name":  "University of London",
		"url":   "https://london.example",
	} {
		if got, _ := school.Get(key); got != want {
			t.Errorf("the school's %s is %v, want %q", key, got, want)
		}
	}
}

func TestDeclaredProfilesJoinSameAsWithoutRepeating(t *testing.T) {
	author := testAuthor()
	author["same_as"] = []any{"https://example.org/ada", "https://elsewhere.example"}
	got, ok := person(t, author).Get("sameAs")
	if !ok {
		t.Fatal("the Person carries no sameAs")
	}
	if strings.Join(got.([]string), ",") != "https://example.org/ada,https://elsewhere.example" {
		t.Errorf("sameAs is %v", got)
	}
}

func TestAProfileTheAuthorNeverDeclaredIsAdded(t *testing.T) {
	got, ok := person(t, testAuthor()).Get("sameAs")
	if !ok {
		t.Fatal("the Person carries no sameAs")
	}
	want := "https://github.com/testauthor,https://example.org/ada"
	if strings.Join(got.([]string), ",") != want {
		t.Errorf("sameAs is %v, want %s", got, want)
	}
}

func TestABuildWithNoAuthorRefuses(t *testing.T) {
	_, err := CVPersonJSONLD(parse(t, document), nil)
	if err == nil || !strings.Contains(err.Error(), "author") {
		t.Errorf("refusal is %v", err)
	}
}

// -- the payload the page carries -------------------------------------------

func TestThePageCarriesThePersonAsAnEncodedPayload(t *testing.T) {
	page, err := RenderCVPage(parse(t, document), testAuthor())
	if err != nil {
		t.Fatalf("RenderCVPage: %v", err)
	}
	if !strings.Contains(page, `<h2 class="cv-name">Ada Lovelace</h2>`) {
		t.Error("the page does not carry the header")
	}
	if !strings.Contains(page, CVPersonAttr) {
		t.Error("the page does not carry the Person attribute")
	}
	extracted, ok, err := ExtractCVPerson(page)
	if err != nil {
		t.Fatalf("ExtractCVPerson: %v", err)
	}
	if !ok {
		t.Fatal("the page carried no Person")
	}
	// The payload is what json.dumps wrote, in declared order.
	if !strings.HasPrefix(extracted, `{"@context": "https://schema.org", "@type": "Person", "name": "Test Author"`) {
		t.Errorf("the payload reads %q", extracted)
	}
	if !strings.Contains(extracted, `"jobTitle": "Analyst"`) {
		t.Errorf("the payload does not carry the job title: %q", extracted)
	}
	// The summary crosses the encoding as it was written, links and all.
	if !strings.Contains(extracted, `"description": "I write [notes](https://example.org/notes) about engines."`) {
		t.Errorf("the payload does not carry the summary verbatim: %q", extracted)
	}
}

func TestAPayloadThatIsNotJSONIsRefused(t *testing.T) {
	_, _, err := ExtractCVPerson(`<div ` + CVPersonAttr + `="bm90IGpzb24="></div>`)
	if err == nil || !strings.Contains(err.Error(), "does not decode") {
		t.Errorf("refusal is %v", err)
	}
}

func TestAPayloadThatIsNotAnObjectIsRefused(t *testing.T) {
	// "[1, 2]" base64-encoded: valid JSON, but not one Person.
	_, _, err := ExtractCVPerson(`<div ` + CVPersonAttr + `="WzEsIDJd"></div>`)
	if err == nil || !strings.Contains(err.Error(), "must hold a JSON object") {
		t.Errorf("refusal is %v", err)
	}
}

func TestAPageWithNoCVCarriesNoPerson(t *testing.T) {
	got, ok, err := ExtractCVPerson("<p>An ordinary page.</p>")
	if err != nil {
		t.Fatalf("ExtractCVPerson: %v", err)
	}
	if ok || got != "" {
		t.Errorf("an ordinary page reported a Person: %q", got)
	}
}

func TestTheRenderedJSONKeepsThePythonsSpelling(t *testing.T) {
	// ", " between items and ": " after a key, non-ASCII escaped, and the
	// properties in declared order rather than sorted.
	got, err := renderJSON(identity.Entity{
		{Key: "@type", Value: "Person"},
		{Key: "name", Value: "Ada Lovelacé"},
		{Key: "sameAs", Value: []string{"https://a.example", "https://b.example"}},
		{Key: "count", Value: int64(2)},
		{Key: "ratio", Value: 1.5},
		{Key: "flag", Value: true},
		{Key: "nothing", Value: nil},
	})
	if err != nil {
		t.Fatalf("renderJSON: %v", err)
	}
	// ensure_ascii is on, so the accented letter crosses as an escape.
	want := `{"@type": "Person", "name": "Ada Lovelac\u00e9", ` +
		`"sameAs": ["https://a.example", "https://b.example"], ` +
		`"count": 2, "ratio": 1.5, "flag": true, "nothing": null}`
	if got != want {
		t.Errorf("rendered\n%s\nwant\n%s", got, want)
	}
}

func TestNumbersRoundTripThroughTheDecoder(t *testing.T) {
	// A decoded document re-renders the way a Python json round-trip does: an
	// integer keeps its digits, and a fraction or exponent goes through the
	// float repr.
	decoded, err := decodeOrderedJSON([]byte(`{"a": 1, "b": 1.50, "c": 1e3}`))
	if err != nil {
		t.Fatalf("decodeOrderedJSON: %v", err)
	}
	got, err := renderJSON(decoded)
	if err != nil {
		t.Fatalf("renderJSON: %v", err)
	}
	if want := `{"a": 1, "b": 1.5, "c": 1000.0}`; got != want {
		t.Errorf("rendered %s, want %s", got, want)
	}
}

// -- parity with the reference engine ---------------------------------------

func TestTheRenderedPageIsTheReferenceEnginesPage(t *testing.T) {
	// The whole body, byte for byte, against the output of the Python this
	// package replaces. Everything the renderer emits is compared at once:
	// the one-line header, the fixed section skeleton, the en dash between a
	// language and its level, the literal ampersand in the interests heading,
	// and the single trailing newline.
	want, err := os.ReadFile(filepath.Join("testdata", "reference-page.md"))
	if err != nil {
		t.Fatal(err)
	}
	if got := RenderCVMarkdown(parse(t, document)); got != string(want) {
		t.Errorf("the rendered page diverges.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestTheRefusalsAreTheReferenceEnginesRefusals(t *testing.T) {
	// Every refusal, word for word, as the Python worded it -- a consumer
	// reads these messages, and the parse is the only thing that explains a
	// malformed CV.
	tests := []struct {
		name string
		text string
		want string
	}{
		{
			"wrong format version",
			strings.Replace(document, "format_version = 1", "format_version = 2", 1),
			"docs/cv.toml declares format_version 2; this selfdoc reads 1.",
		},
		{
			"no format version",
			strings.Replace(document, "format_version = 1", "", 1),
			"docs/cv.toml declares format_version None; this selfdoc reads 1.",
		},
		{
			"unknown top-level key",
			document + "\n[publications]\nbody = \"none\"\n",
			"docs/cv.toml declares unknown key(s) 'publications'. It carries " +
				"format_version, identity, skills, projects, interests, " +
				"education, experience, languages, contact.",
		},
		{
			"unknown identity key",
			strings.Replace(document, `name = "Ada Lovelace"`, "name = \"Ada\"\nnickname = \"AAL\"", 1),
			"docs/cv.toml: [identity] declares unknown key(s) 'nickname'. It " +
				"carries name, headline, location, email, photo, summary, " +
				"updated, profile.",
		},
		{
			"unknown block key",
			strings.Replace(document, `focus = "Mathematics"`, "focus = \"Mathematics\"\ngrade = \"110\"", 1),
			"docs/cv.toml: [[education]] #1 declares unknown key(s) 'grade'. " +
				"It carries degree, years, institute, institute_url, " +
				"location, focus, thesis, course_url.",
		},
		{
			"missing required field",
			strings.Replace(document, "email = \"ada@example.org\"\n", "", 1),
			"docs/cv.toml: [identity] is missing a non-empty 'email'.",
		},
		{
			"empty required field",
			strings.Replace(document, `headline = "Analyst"`, `headline = "  "`, 1),
			"docs/cv.toml: [identity] is missing a non-empty 'headline'.",
		},
		{
			"repeated skill category",
			strings.Replace(document, `category = "Other"`, `category = "Languages"`, 1),
			"docs/cv.toml: [[skills]] #2 repeats the skill category 'Languages'.",
		},
		{
			"repeated education entry",
			document + "\n[[education]]\ndegree = \"Private tuition in mathematics\"\n" +
				"years = \"1841 - 1842\"\ninstitute = \"University of London\"\n" +
				"location = \"London, England\"\n",
			"docs/cv.toml: [[education]] #2 repeats the education entry " +
				"'Private tuition in mathematics at University of London'.",
		},
		{
			"repeated post",
			document + "\n[[experience]]\nrole = \"Correspondent\"\nperiod = \"1840\"\n" +
				"company = \"Self-employed\"\nlocation = \"England\"\n",
			"docs/cv.toml: [[experience]] #3 repeats the post " +
				"'Correspondent at Self-employed (1840)'.",
		},
		{
			"project with nothing to say",
			strings.Replace(document,
				"name = \"Translation\"\ntechnologies = [\"French\"]",
				`name = "Translation"`, 1),
			"docs/cv.toml: [[projects]] #2 (Translation) declares neither " +
				"'notes' nor 'technologies', so the entry would be a bare heading.",
		},
		{
			"absent section",
			without("languages"),
			"docs/cv.toml declares no [[languages]] block. Every CV section " +
				"is declared and non-empty; an absent one would render as a " +
				"heading over nothing.",
		},
	}
	for _, test := range tests {
		_, err := ParseCV(test.text, CVSource)
		if err == nil {
			t.Errorf("%s: the document was accepted", test.name)
			continue
		}
		if err.Error() != test.want {
			t.Errorf("%s: refused with\n%s\nwant\n%s", test.name, err, test.want)
		}
	}
}

func TestThePersonIsTheReferenceEnginesPerson(t *testing.T) {
	// The whole JSON-LD document, byte for byte, as the Python's json.dumps
	// wrote it: declared property order, ", " and ": " separators, and the
	// CV's own profile folded in after the author's declared ones.
	entity, err := CVPersonJSONLD(parse(t, document), testAuthor())
	if err != nil {
		t.Fatalf("CVPersonJSONLD: %v", err)
	}
	got, err := renderJSON(entity)
	if err != nil {
		t.Fatalf("renderJSON: %v", err)
	}
	want := `{"@context": "https://schema.org", "@type": "Person", ` +
		`"name": "Test Author", "url": "https://author.example", ` +
		`"sameAs": ["https://github.com/testauthor", "https://example.org/ada"], ` +
		`"alternateName": "Ada Lovelace", "jobTitle": "Analyst", ` +
		`"description": "I write [notes](https://example.org/notes) about engines.", ` +
		`"email": "mailto:ada@example.org", ` +
		`"address": {"@type": "PostalAddress", "addressLocality": "London, England"}, ` +
		`"knowsLanguage": [{"@type": "Language", "name": "English"}, ` +
		`{"@type": "Language", "name": "French"}], ` +
		`"alumniOf": [{"@type": "EducationalOrganization", ` +
		`"name": "University of London", "url": "https://london.example"}]}`
	if got != want {
		t.Errorf("emitted\n%s\nwant\n%s", got, want)
	}
}
