// Package cv holds the CV as data: one declared document, rendered as a page
// and as a Person.
//
// A curriculum vitae is a record, not prose that happens to look like one --
// every part of it is a field somebody could ask for by name. It is therefore
// declared in a TOML document and rendered from there, so the page a reader
// sees and the Person a crawler reads are two renderings of one source rather
// than two texts that have to be kept in agreement by hand.
//
// The document is validated strictly: an unknown key anywhere, a missing
// required field, or an empty section is a hard error naming the offending
// declaration. Every section is required and non-empty, for the reason the
// curated project listing gives for the same rule -- an absent section would
// render as a heading over nothing, and there is no sensible default for a
// fact about a person.
//
// Two renderings, both from [CV]:
//
//   - [RenderCVMarkdown] -- the page body, as Markdown, which the build
//     converts like any other page content.
//   - [CVPersonJSONLD] -- a Person carrying what the CV knows on top of the
//     site's declared author: the job title, the summary, the languages, the
//     schools, and every external profile.
package cv

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"bytes"

	"github.com/stricttools/selfdoc/internal/identity"
	"github.com/stricttools/selfdoc/internal/util"
)

// CVSource is where the home project declares its CV, relative to the project
// root.
const CVSource = "docs/cv.toml"

// CVFormatVersion is the document format this package reads. A document
// carrying anything else is a hard error, never a guess about which shape was
// meant.
const CVFormatVersion int64 = 1

// CVPageType is the page type a CV page declares in its frontmatter, which the
// schema-type mapping turns into ProfilePage.
const CVPageType = "cv"

// The fixed key sets of each declaration. A block carrying a key its set does
// not name is refused, so a typo is reported rather than silently ignored.
var (
	// TopLevelKeys is every key the document itself may carry.
	TopLevelKeys = []string{
		"format_version", "identity", "skills", "projects", "interests",
		"education", "experience", "languages", "contact",
	}
	// IdentityKeys is every key the [identity] table may carry.
	IdentityKeys = []string{
		"name", "headline", "location", "email", "photo", "summary",
		"updated", "profile",
	}
	// ProfileKeys is every key an [[identity.profile]] block may carry.
	ProfileKeys = []string{"label", "url"}
	// SkillKeys is every key a [[skills]] block may carry.
	SkillKeys = []string{"category", "items"}
	// ProjectKeys is every key a [[projects]] block may carry.
	ProjectKeys = []string{"name", "notes", "technologies"}
	// InterestKeys is every key an [[interests]] block may carry.
	InterestKeys = []string{"title", "body"}
	// EducationKeys is every key an [[education]] block may carry.
	EducationKeys = []string{
		"degree", "years", "institute", "institute_url", "location", "focus",
		"thesis", "course_url",
	}
	// ExperienceKeys is every key an [[experience]] block may carry.
	ExperienceKeys = []string{
		"role", "period", "company", "company_url", "location", "body",
	}
	// LanguageKeys is every key a [[languages]] block may carry.
	LanguageKeys = []string{"name", "url", "level"}
	// ContactKeys is every key the [contact] table may carry.
	ContactKeys = []string{"body"}
)

// Profile is one external address the CV's owner is reachable at.
type Profile struct {
	// Label is how the address is named on the page.
	Label string
	// URL is the address itself.
	URL string
}

// Identity is who the CV is about.
type Identity struct {
	// Name is the person's name, as the CV spells it.
	Name string
	// Headline is the one-line job title the header carries.
	Headline string
	// Location is where the person is.
	Location string
	// Email is the address the header links to.
	Email string
	// Summary is the opening paragraph, as Markdown.
	Summary string
	// Photo is the portrait's source, empty when the CV declares none.
	Photo string
	// Updated is the CV's own closing date, empty when it declares none.
	Updated string
	// Profiles are the external addresses the CV lists.
	Profiles []Profile
}

// SkillGroup is one category of skills with its items.
type SkillGroup struct {
	// Category names the group.
	Category string
	// Items are the skills in it.
	Items []string
}

// Project is one project the CV lists.
type Project struct {
	// Name is the project's name.
	Name string
	// Notes are the bullet points under it.
	Notes []string
	// Technologies are what it was built with.
	Technologies []string
}

// Interest is one hobby or interest with its description.
type Interest struct {
	// Title names the interest.
	Title string
	// Body describes it, as Markdown.
	Body string
}

// Education is one qualification.
type Education struct {
	// Degree is the qualification's name.
	Degree string
	// Years is when it was studied.
	Years string
	// Institute is where.
	Institute string
	// Location is the institute's location.
	Location string
	// InstituteURL links the institute, empty when none is declared.
	InstituteURL string
	// Focus is the subject emphasis, empty when none is declared.
	Focus string
	// Thesis is the thesis title, empty when none is declared.
	Thesis string
	// CourseURL links the course, empty when none is declared.
	CourseURL string
}

// Experience is one post held.
type Experience struct {
	// Role is the job title.
	Role string
	// Period is when it was held.
	Period string
	// Company is the employer.
	Company string
	// Location is where the post was.
	Location string
	// CompanyURL links the employer, empty when none is declared.
	CompanyURL string
	// Body describes the post, as Markdown, empty when none is declared.
	Body string
}

// Language is one language spoken, with the level and an optional link.
type Language struct {
	// Name is the language.
	Name string
	// Level is the declared proficiency.
	Level string
	// URL links the language, empty when none is declared.
	URL string
}

// CV is the whole declared document, in declared order.
type CV struct {
	// Identity is who the CV is about.
	Identity Identity
	// Skills are the declared skill groups.
	Skills []SkillGroup
	// Projects are the declared projects.
	Projects []Project
	// Interests are the declared hobbies and interests.
	Interests []Interest
	// Education are the declared qualifications.
	Education []Education
	// Experience are the declared posts.
	Experience []Experience
	// Languages are the declared languages.
	Languages []Language
	// Contact is how a reader reaches the person, as Markdown.
	Contact string
}

// -- parsing ----------------------------------------------------------------

// rejectUnknown refuses a block carrying a key its declared set does not name.
func rejectUnknown(where string, block map[string]any, known []string) error {
	permitted := make(map[string]bool, len(known))
	for _, key := range known {
		permitted[key] = true
	}
	var unknown []string
	for key := range block {
		if !permitted[key] {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	quoted := make([]string, len(unknown))
	for index, key := range unknown {
		quoted[index] = util.PythonRepr(key)
	}
	return fmt.Errorf(
		"%s declares unknown key(s) %s. It carries %s.",
		where, strings.Join(quoted, ", "), strings.Join(known, ", "),
	)
}

// textField returns the stripped string a block declares at key.
//
// A key the block does not carry is the empty string when the field is
// optional and a refusal when it is required; a value that is not a string, or
// is only whitespace, is a refusal either way.
func textField(where string, block map[string]any, key string, required bool) (string, error) {
	value, present := block[key]
	if !present || value == nil {
		if required {
			return "", fmt.Errorf("%s is missing a non-empty %s.", where, util.PythonRepr(key))
		}
		return "", nil
	}
	text, ok := value.(string)
	if !ok || util.PythonStrip(text) == "" {
		return "", fmt.Errorf("%s is missing a non-empty %s.", where, util.PythonRepr(key))
	}
	return util.PythonStrip(text), nil
}

// stringsField returns the stripped strings a block declares at key.
//
// An absent key is an empty list when the field is optional and a refusal when
// it is required; a value that is not a non-empty list of non-empty strings is
// a refusal either way.
func stringsField(where string, block map[string]any, key string, required bool) ([]string, error) {
	value, present := block[key]
	if !present || value == nil {
		if required {
			return nil, fmt.Errorf(
				"%s is missing %s, a non-empty list of strings.",
				where, util.PythonRepr(key),
			)
		}
		return nil, nil
	}
	list, ok := asList(value)
	if !ok || len(list) == 0 {
		return nil, fmt.Errorf(
			"%s: %s must be a non-empty list of strings.", where, util.PythonRepr(key),
		)
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		text, isText := item.(string)
		if !isText || util.PythonStrip(text) == "" {
			return nil, fmt.Errorf(
				"%s: every entry of %s must be a non-empty string.",
				where, util.PythonRepr(key),
			)
		}
		out = append(out, util.PythonStrip(text))
	}
	return out, nil
}

// rejectRepeat refuses a section entry that repeats one already declared.
//
// A CV lists each thing once: a repeated entry renders twice and states the
// same fact twice, which is a mistake in the document rather than something to
// render faithfully. key is what identifies the entry -- one field where that
// is enough, several where it is not -- and label is how the entry is named
// back to whoever wrote it.
func rejectRepeat(where string, seen map[string]bool, key []string, label, kind string) error {
	// The parts are joined on a character no declaration can carry, so two
	// different tuples can never collide into one identity.
	identifier := strings.Join(key, "\x00")
	if seen[identifier] {
		return fmt.Errorf("%s repeats the %s %s.", where, kind, util.PythonRepr(label))
	}
	seen[identifier] = true
	return nil
}

// blockRef is one [[key]] block with the place to name in errors about it.
type blockRef struct {
	// Where names the block in a diagnostic.
	Where string
	// Block is the declared table.
	Block map[string]any
}

// blocks returns the [[key]] blocks a document declares, each with the place
// to name in errors.
func blocks(source string, data map[string]any, key string) ([]blockRef, error) {
	list, ok := asList(data[key])
	if !ok || len(list) == 0 {
		return nil, fmt.Errorf(
			"%s declares no [[%s]] block. Every CV section is declared and "+
				"non-empty; an absent one would render as a heading over "+
				"nothing.", source, key,
		)
	}
	out := make([]blockRef, 0, len(list))
	for index, item := range list {
		where := fmt.Sprintf("%s: [[%s]] #%d", source, key, index+1)
		block, isTable := asMap(item)
		if !isTable {
			return nil, fmt.Errorf("%s is not a table.", where)
		}
		out = append(out, blockRef{Where: where, Block: block})
	}
	return out, nil
}

// ParseCV returns the CV the document text declares, naming source in every
// diagnostic.
//
// It refuses, naming the offending declaration, a syntax error, an unknown
// key, a missing or empty required field, an empty section, a repeated entry,
// or a format version this package does not read.
func ParseCV(text string, source string) (*CV, error) {
	data, err := util.DecodeTOML([]byte(text))
	if err != nil {
		return nil, fmt.Errorf("%s is not valid TOML: %s", source, err)
	}

	if err := rejectUnknown(source, data, TopLevelKeys); err != nil {
		return nil, err
	}

	version, ok := data["format_version"].(int64)
	if !ok || version != CVFormatVersion {
		return nil, fmt.Errorf(
			"%s declares format_version %s; this selfdoc reads %d.",
			source, util.PythonRepr(data["format_version"]), CVFormatVersion,
		)
	}

	rawIdentity, ok := asMap(data["identity"])
	if !ok {
		return nil, fmt.Errorf(
			"%s declares no [identity] table -- the name, headline, "+
				"location, email and summary the page opens with.", source,
		)
	}
	where := source + ": [identity]"
	if err := rejectUnknown(where, rawIdentity, IdentityKeys); err != nil {
		return nil, err
	}
	rawProfiles := rawIdentity["profile"]
	if !truthy(rawProfiles) {
		rawProfiles = []any{}
	}
	profileList, ok := asList(rawProfiles)
	if !ok {
		return nil, fmt.Errorf("%s: 'profile' must be a list of tables.", where)
	}
	var profiles []Profile
	for index, item := range profileList {
		spot := fmt.Sprintf("%s: [[identity.profile]] #%d", source, index+1)
		block, isTable := asMap(item)
		if !isTable {
			return nil, fmt.Errorf("%s is not a table.", spot)
		}
		if err := rejectUnknown(spot, block, ProfileKeys); err != nil {
			return nil, err
		}
		label, err := textField(spot, block, "label", true)
		if err != nil {
			return nil, err
		}
		url, err := textField(spot, block, "url", true)
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, Profile{Label: label, URL: url})
	}
	person := Identity{Profiles: profiles}
	for _, field := range []struct {
		target   *string
		key      string
		required bool
	}{
		{&person.Name, "name", true},
		{&person.Headline, "headline", true},
		{&person.Location, "location", true},
		{&person.Email, "email", true},
		{&person.Summary, "summary", true},
		{&person.Photo, "photo", false},
		{&person.Updated, "updated", false},
	} {
		value, err := textField(where, rawIdentity, field.key, field.required)
		if err != nil {
			return nil, err
		}
		*field.target = value
	}

	var skills []SkillGroup
	seenCategories := map[string]bool{}
	sections, err := blocks(source, data, "skills")
	if err != nil {
		return nil, err
	}
	for _, section := range sections {
		if err := rejectUnknown(section.Where, section.Block, SkillKeys); err != nil {
			return nil, err
		}
		category, err := textField(section.Where, section.Block, "category", true)
		if err != nil {
			return nil, err
		}
		if err := rejectRepeat(
			section.Where, seenCategories, []string{category}, category,
			"skill category",
		); err != nil {
			return nil, err
		}
		items, err := stringsField(section.Where, section.Block, "items", true)
		if err != nil {
			return nil, err
		}
		skills = append(skills, SkillGroup{Category: category, Items: items})
	}

	var projects []Project
	seenProjects := map[string]bool{}
	sections, err = blocks(source, data, "projects")
	if err != nil {
		return nil, err
	}
	for _, section := range sections {
		if err := rejectUnknown(section.Where, section.Block, ProjectKeys); err != nil {
			return nil, err
		}
		name, err := textField(section.Where, section.Block, "name", true)
		if err != nil {
			return nil, err
		}
		if err := rejectRepeat(
			section.Where, seenProjects, []string{name}, name, "project",
		); err != nil {
			return nil, err
		}
		notes, err := stringsField(section.Where, section.Block, "notes", false)
		if err != nil {
			return nil, err
		}
		technologies, err := stringsField(section.Where, section.Block, "technologies", false)
		if err != nil {
			return nil, err
		}
		if len(notes) == 0 && len(technologies) == 0 {
			return nil, fmt.Errorf(
				"%s (%s) declares neither 'notes' nor 'technologies', so the "+
					"entry would be a bare heading.", section.Where, name,
			)
		}
		projects = append(projects, Project{
			Name: name, Notes: notes, Technologies: technologies,
		})
	}

	var interests []Interest
	seenInterests := map[string]bool{}
	sections, err = blocks(source, data, "interests")
	if err != nil {
		return nil, err
	}
	for _, section := range sections {
		if err := rejectUnknown(section.Where, section.Block, InterestKeys); err != nil {
			return nil, err
		}
		title, err := textField(section.Where, section.Block, "title", true)
		if err != nil {
			return nil, err
		}
		if err := rejectRepeat(
			section.Where, seenInterests, []string{title}, title, "interest",
		); err != nil {
			return nil, err
		}
		body, err := textField(section.Where, section.Block, "body", true)
		if err != nil {
			return nil, err
		}
		interests = append(interests, Interest{Title: title, Body: body})
	}

	var education []Education
	seenEducation := map[string]bool{}
	sections, err = blocks(source, data, "education")
	if err != nil {
		return nil, err
	}
	for _, section := range sections {
		if err := rejectUnknown(section.Where, section.Block, EducationKeys); err != nil {
			return nil, err
		}
		degree, err := textField(section.Where, section.Block, "degree", true)
		if err != nil {
			return nil, err
		}
		institute, err := textField(section.Where, section.Block, "institute", true)
		if err != nil {
			return nil, err
		}
		// Degree and school together identify the entry: two degrees from one
		// university, and one degree from two universities, are both
		// ordinary; the same degree twice at the same place is a mistake.
		if err := rejectRepeat(
			section.Where, seenEducation, []string{degree, institute},
			degree+" at "+institute, "education entry",
		); err != nil {
			return nil, err
		}
		entry := Education{Degree: degree, Institute: institute}
		for _, field := range []struct {
			target   *string
			key      string
			required bool
		}{
			{&entry.Years, "years", true},
			{&entry.Location, "location", true},
			{&entry.InstituteURL, "institute_url", false},
			{&entry.Focus, "focus", false},
			{&entry.Thesis, "thesis", false},
			{&entry.CourseURL, "course_url", false},
		} {
			value, err := textField(section.Where, section.Block, field.key, field.required)
			if err != nil {
				return nil, err
			}
			*field.target = value
		}
		education = append(education, entry)
	}

	var experience []Experience
	seenExperience := map[string]bool{}
	sections, err = blocks(source, data, "experience")
	if err != nil {
		return nil, err
	}
	for _, section := range sections {
		if err := rejectUnknown(section.Where, section.Block, ExperienceKeys); err != nil {
			return nil, err
		}
		role, err := textField(section.Where, section.Block, "role", true)
		if err != nil {
			return nil, err
		}
		company, err := textField(section.Where, section.Block, "company", true)
		if err != nil {
			return nil, err
		}
		period, err := textField(section.Where, section.Block, "period", true)
		if err != nil {
			return nil, err
		}
		// Two stints in one role at one employer are a real career shape, so
		// the period is part of the identity; all three the same is one post
		// declared twice.
		if err := rejectRepeat(
			section.Where, seenExperience, []string{role, company, period},
			fmt.Sprintf("%s at %s (%s)", role, company, period), "post",
		); err != nil {
			return nil, err
		}
		entry := Experience{Role: role, Period: period, Company: company}
		for _, field := range []struct {
			target   *string
			key      string
			required bool
		}{
			{&entry.Location, "location", true},
			{&entry.CompanyURL, "company_url", false},
			{&entry.Body, "body", false},
		} {
			value, err := textField(section.Where, section.Block, field.key, field.required)
			if err != nil {
				return nil, err
			}
			*field.target = value
		}
		experience = append(experience, entry)
	}

	var languages []Language
	seenLanguages := map[string]bool{}
	sections, err = blocks(source, data, "languages")
	if err != nil {
		return nil, err
	}
	for _, section := range sections {
		if err := rejectUnknown(section.Where, section.Block, LanguageKeys); err != nil {
			return nil, err
		}
		name, err := textField(section.Where, section.Block, "name", true)
		if err != nil {
			return nil, err
		}
		if err := rejectRepeat(
			section.Where, seenLanguages, []string{name}, name, "language",
		); err != nil {
			return nil, err
		}
		level, err := textField(section.Where, section.Block, "level", true)
		if err != nil {
			return nil, err
		}
		url, err := textField(section.Where, section.Block, "url", false)
		if err != nil {
			return nil, err
		}
		languages = append(languages, Language{Name: name, Level: level, URL: url})
	}

	rawContact, ok := asMap(data["contact"])
	if !ok {
		return nil, fmt.Errorf(
			"%s declares no [contact] table -- how a reader reaches the "+
				"person the CV is about.", source,
		)
	}
	contactWhere := source + ": [contact]"
	if err := rejectUnknown(contactWhere, rawContact, ContactKeys); err != nil {
		return nil, err
	}
	contact, err := textField(contactWhere, rawContact, "body", true)
	if err != nil {
		return nil, err
	}

	return &CV{
		Identity:   person,
		Skills:     skills,
		Projects:   projects,
		Interests:  interests,
		Education:  education,
		Experience: experience,
		Languages:  languages,
		Contact:    contact,
	}, nil
}

// LoadCV returns the CV declared in the TOML document at path.
func LoadCV(path string) (*CV, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseCV(string(raw), path)
}

// -- rendering --------------------------------------------------------------

// link renders text as a Markdown link to url, or as plain text when there is
// no url.
func link(text, url string) string {
	if url == "" {
		return text
	}
	return "[" + text + "](" + url + ")"
}

// renderCVHeader returns the CV's header block: the photo beside who the
// person is.
//
// This is HTML rather than Markdown because it is structure, not prose: a
// portrait, a name, and a row of details separated from one another. The
// themes style it through the "cv-" classes it carries, and the whole block is
// emitted on one physical line so the Markdown converter passes it through
// whole.
//
// The name is a level-2 heading: the page's own title is its h1, and the name
// introduces the document under it.
func renderCVHeader(person Identity) string {
	photo := ""
	if person.Photo != "" {
		photo = `<div class="cv-photo">` +
			`<img src="` + util.EscapeHTML(person.Photo) + `" alt="Profile picture">` +
			`</div>`
	}

	details := []string{
		util.EscapeHTML(person.Headline),
		util.EscapeHTML(person.Location),
		`<a href="mailto:` + util.EscapeHTML(person.Email) + `">` +
			util.EscapeHTML(person.Email) + `</a>`,
	}
	for _, profile := range person.Profiles {
		details = append(details, `<a href="`+util.EscapeHTML(profile.URL)+`">`+
			util.EscapeHTML(profile.Label)+`</a>`)
	}
	var subtitle strings.Builder
	for _, detail := range details {
		subtitle.WriteString("<span>" + detail + "</span>")
	}

	return `<div class="cv-header">` +
		photo +
		`<div class="cv-identity">` +
		`<h2 class="cv-name">` + util.EscapeHTML(person.Name) + `</h2>` +
		`<div class="cv-subtitle">` + subtitle.String() + `</div>` +
		`</div>` +
		`</div>`
}

// RenderCVMarkdown returns the CV as the Markdown body of a page.
//
// Section headings are fixed, because they are the document's structure rather
// than one of its facts. The header block and the closing date are HTML, for
// the reason [renderCVHeader] gives; everything else is Markdown, so the
// headings enter the table of contents and the prose keeps its links and
// emphasis.
func RenderCVMarkdown(cv *CV) string {
	person := cv.Identity
	parts := []string{renderCVHeader(person), "", person.Summary, ""}

	parts = append(parts, "## Skills", "")
	for _, group := range cv.Skills {
		parts = append(parts, "- **"+group.Category+":** "+strings.Join(group.Items, ", "))
	}
	parts = append(parts, "")

	parts = append(parts, "## Projects", "")
	for _, project := range cv.Projects {
		parts = append(parts, "### "+project.Name, "")
		for _, note := range project.Notes {
			parts = append(parts, "- "+note)
		}
		if len(project.Technologies) > 0 {
			parts = append(parts,
				"- **Technologies used:** "+strings.Join(project.Technologies, ", "))
		}
		parts = append(parts, "")
	}

	parts = append(parts, "## Hobbies & interests", "")
	for _, interest := range cv.Interests {
		parts = append(parts, "### "+interest.Title, "", interest.Body, "")
	}

	parts = append(parts, "## Education", "")
	for _, entry := range cv.Education {
		parts = append(parts, "### "+entry.Degree, "")
		parts = append(parts, "- **Year:** "+entry.Years)
		parts = append(parts, "- **Institute:** "+link(entry.Institute, entry.InstituteURL))
		parts = append(parts, "- **Location:** "+entry.Location)
		if entry.Focus != "" {
			parts = append(parts, "- **Focus:** "+entry.Focus)
		}
		if entry.Thesis != "" {
			parts = append(parts, "- **Thesis:** "+entry.Thesis)
		}
		if entry.CourseURL != "" {
			parts = append(parts, "- [Course details]("+entry.CourseURL+")")
		}
		parts = append(parts, "")
	}

	parts = append(parts, "## Work experience", "")
	for _, entry := range cv.Experience {
		parts = append(parts, "### "+entry.Role, "")
		parts = append(parts, "- **Period:** "+entry.Period)
		parts = append(parts, "- **Company:** "+link(entry.Company, entry.CompanyURL))
		parts = append(parts, "- **Location:** "+entry.Location)
		parts = append(parts, "")
		if entry.Body != "" {
			parts = append(parts, entry.Body, "")
		}
	}

	parts = append(parts, "## Languages", "")
	for _, language := range cv.Languages {
		parts = append(parts, "- "+link(language.Name, language.URL)+" – "+language.Level)
	}
	parts = append(parts, "")

	parts = append(parts, "## Contact information", "", cv.Contact, "")

	if person.Updated != "" {
		parts = append(parts,
			`<div class="cv-updated">Last updated on `+
				util.EscapeHTML(person.Updated)+`</div>`,
			"",
		)
	}

	return strings.TrimRight(strings.Join(parts, "\n"), "\n") + "\n"
}

// CVPersonJSONLD returns the Person a CV page states, as a JSON-LD document.
//
// The identity itself -- name, url, sameAs -- comes from the site's declared
// author, so a CV page and the front page name the same person. What the CV
// adds is what a CV knows: the job title, the summary, the languages, the
// schools, and the external profiles it lists (folded into sameAs after the
// declared ones, without repeating any).
//
// It returns [identity.ErrNoDeclaredAuthor] when the build declares no author.
func CVPersonJSONLD(cv *CV, author map[string]any) (identity.Entity, error) {
	var sameAs []string
	if author != nil {
		if declared, ok := asList(author["same_as"]); ok {
			for _, entry := range declared {
				sameAs = append(sameAs, util.PythonStr(entry))
			}
		}
	}
	for _, profile := range cv.Identity.Profiles {
		if !contains(sameAs, profile.URL) {
			sameAs = append(sameAs, profile.URL)
		}
	}

	// The identity's name is the site's, so it stays the same on every page; a
	// CV that spells the same person's name out more fully contributes that
	// spelling as an alternate rather than a second entity with a different
	// name.
	alternateName := cv.Identity.Name
	if author != nil {
		if declaredName, ok := author["name"].(string); ok && declaredName == cv.Identity.Name {
			alternateName = ""
		}
	}

	knowsLanguage := make([]any, 0, len(cv.Languages))
	for _, language := range cv.Languages {
		knowsLanguage = append(knowsLanguage, identity.Entity{
			{Key: "@type", Value: "Language"},
			{Key: "name", Value: language.Name},
		})
	}
	alumniOf := make([]any, 0, len(cv.Education))
	for _, entry := range cv.Education {
		school := identity.Entity{
			{Key: "@type", Value: "EducationalOrganization"},
			{Key: "name", Value: entry.Institute},
		}
		if entry.InstituteURL != "" {
			school = append(school, identity.Property{Key: "url", Value: entry.InstituteURL})
		}
		alumniOf = append(alumniOf, school)
	}

	extra := identity.Entity{
		{Key: "alternateName", Value: alternateName},
		{Key: "jobTitle", Value: cv.Identity.Headline},
		{Key: "description", Value: cv.Identity.Summary},
		{Key: "email", Value: "mailto:" + cv.Identity.Email},
		{Key: "address", Value: identity.Entity{
			{Key: "@type", Value: "PostalAddress"},
			{Key: "addressLocality", Value: cv.Identity.Location},
		}},
		{Key: "knowsLanguage", Value: knowsLanguage},
		{Key: "alumniOf", Value: alumniOf},
	}

	declaredAuthor := map[string]any{}
	for key, value := range author {
		declaredAuthor[key] = value
	}
	declaredAuthor["same_as"] = sameAs
	return identity.PersonEntity(declaredAuthor, true, extra)
}

// contains reports whether values already holds candidate.
func contains(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

// CVPersonAttr is the attribute the rendered page carries its Person in, for
// the SEO tag builder to lift into the head.
//
// A directive resolves BEFORE the Markdown converter runs, so anything it
// emits is text the converter will rewrite: a JSON-LD script in body position
// comes back with the summary's link turned into an anchor tag inside a JSON
// string, and an HTML-escaped attribute fares no better, because the inline
// transforms run across attribute values too. The payload therefore crosses
// the conversion base64-encoded -- an alphabet with no Markdown meaning -- and
// is decoded into real structured data in the head, where the page's other
// JSON-LD is emitted.
const CVPersonAttr = "data-cv-person"

// personAttrRe locates the payload the rendered page carries.
var personAttrRe = regexp.MustCompile(CVPersonAttr + `="([A-Za-z0-9+/=]*)"`)

// RenderCVPage returns the page body: the CV, carrying the Person it states.
func RenderCVPage(cv *CV, author map[string]any) (string, error) {
	entity, err := CVPersonJSONLD(cv, author)
	if err != nil {
		return "", err
	}
	person, err := renderJSON(entity)
	if err != nil {
		return "", err
	}
	payload := base64.StdEncoding.EncodeToString([]byte(person))
	marker := `<div class="cv-person" ` + CVPersonAttr + `="` + payload + `"></div>`
	return marker + "\n\n" + RenderCVMarkdown(cv), nil
}

// ExtractCVPerson returns the Person JSON a rendered CV page carries. The bool
// reports whether the page carried one at all.
//
// It refuses when the attribute is there but does not decode to a JSON object
// -- a page that carried a broken entity would publish it as if it were a
// fact.
func ExtractCVPerson(bodyHTML string) (string, bool, error) {
	match := personAttrRe.FindStringSubmatch(bodyHTML)
	if match == nil {
		return "", false, nil
	}
	raw, err := base64.StdEncoding.DecodeString(match[1])
	if err != nil {
		return "", false, fmt.Errorf(
			"the CV page's %s attribute does not decode to JSON: %s",
			CVPersonAttr, err,
		)
	}
	decoded, err := decodeOrderedJSON(raw)
	if err != nil {
		return "", false, fmt.Errorf(
			"the CV page's %s attribute does not decode to JSON: %s",
			CVPersonAttr, err,
		)
	}
	entity, ok := decoded.(identity.Entity)
	if !ok {
		return "", false, fmt.Errorf(
			"the CV page's %s attribute must hold a JSON object naming one "+
				"Person.", CVPersonAttr,
		)
	}
	rendered, err := renderJSON(entity)
	if err != nil {
		return "", false, err
	}
	return rendered, true, nil
}

// -- the document's bytes ---------------------------------------------------

// renderJSON encodes value the way Python's json.dumps does with its default
// settings: ", " between items, ": " between a key and its value, non-ASCII
// escaped as \uXXXX, and an object's properties in declared order rather than
// sorted.
//
// The order is the point. The payload is a schema.org entity whose properties
// read "@context", "@type", "name", "url", "sameAs" and then what the page
// contributed, and a sorting encoder would publish them alphabetically.
func renderJSON(value any) (string, error) {
	var out strings.Builder
	if err := writeJSON(&out, value); err != nil {
		return "", err
	}
	return out.String(), nil
}

// jsonNumber is a number as its JSON source spelled it, so a decoded document
// re-encodes the way Python's json round-trip does: an integer literal keeps
// its digits, and anything with a fraction or an exponent renders through the
// float repr.
type jsonNumber string

// writeJSON writes one value in Python's json.dumps spelling.
func writeJSON(out *strings.Builder, value any) error {
	switch typed := value.(type) {
	case nil:
		out.WriteString("null")
	case bool:
		if typed {
			out.WriteString("true")
		} else {
			out.WriteString("false")
		}
	case string:
		out.WriteString(util.PythonJSONString(typed))
	case int:
		out.WriteString(strconv.Itoa(typed))
	case int64:
		out.WriteString(strconv.FormatInt(typed, 10))
	case float64:
		out.WriteString(util.PythonFloatRepr(typed))
	case jsonNumber:
		out.WriteString(renderNumber(string(typed)))
	case identity.Entity:
		out.WriteString("{")
		for index, property := range typed {
			if index > 0 {
				out.WriteString(", ")
			}
			out.WriteString(util.PythonJSONString(property.Key) + ": ")
			if err := writeJSON(out, property.Value); err != nil {
				return err
			}
		}
		out.WriteString("}")
	case []any:
		out.WriteString("[")
		for index, item := range typed {
			if index > 0 {
				out.WriteString(", ")
			}
			if err := writeJSON(out, item); err != nil {
				return err
			}
		}
		out.WriteString("]")
	case []string:
		out.WriteString("[")
		for index, item := range typed {
			if index > 0 {
				out.WriteString(", ")
			}
			out.WriteString(util.PythonJSONString(item))
		}
		out.WriteString("]")
	default:
		return fmt.Errorf("cannot render %T as JSON", value)
	}
	return nil
}

// renderNumber re-renders a JSON number literal the way a Python json
// round-trip does.
func renderNumber(literal string) string {
	if !strings.ContainsAny(literal, ".eE") {
		if parsed, err := strconv.ParseInt(literal, 10, 64); err == nil {
			return strconv.FormatInt(parsed, 10)
		}
		// Beyond int64 Python keeps every digit, and so does the literal.
		return literal
	}
	parsed, err := strconv.ParseFloat(literal, 64)
	if err != nil {
		return literal
	}
	return util.PythonFloatRepr(parsed)
}

// decodeOrderedJSON decodes raw, keeping each object's properties in the order
// the document declares them.
//
// encoding/json decodes an object into an unordered map, which would lose the
// entity's property order; the token stream keeps it.
func decodeOrderedJSON(raw []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	value, err := decodeValue(decoder)
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("trailing data after the JSON document")
	}
	return value, nil
}

// decodeValue decodes the next value from the token stream.
func decodeValue(decoder *json.Decoder) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	return decodeToken(decoder, token)
}

// decodeToken decodes the value that token opens.
func decodeToken(decoder *json.Decoder, token json.Token) (any, error) {
	switch typed := token.(type) {
	case json.Delim:
		switch typed {
		case '{':
			entity := identity.Entity{}
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return nil, err
				}
				key, ok := keyToken.(string)
				if !ok {
					return nil, errors.New("a JSON object carries a non-string key")
				}
				value, err := decodeValue(decoder)
				if err != nil {
					return nil, err
				}
				entity = append(entity, identity.Property{Key: key, Value: value})
			}
			if _, err := decoder.Token(); err != nil {
				return nil, err
			}
			return entity, nil
		case '[':
			items := []any{}
			for decoder.More() {
				item, err := decodeValue(decoder)
				if err != nil {
					return nil, err
				}
				items = append(items, item)
			}
			if _, err := decoder.Token(); err != nil {
				return nil, err
			}
			return items, nil
		}
		return nil, fmt.Errorf("unexpected JSON delimiter %q", typed)
	case json.Number:
		return jsonNumber(typed.String()), nil
	default:
		return token, nil
	}
}

// -- Python value helpers ---------------------------------------------------

// asList returns value as a list, accepting the two shapes the TOML decoder
// produces: an array of scalars is []any, while an array of tables is
// []map[string]any.
func asList(value any) ([]any, bool) {
	switch typed := value.(type) {
	case []any:
		return typed, true
	case []map[string]any:
		out := make([]any, len(typed))
		for index, item := range typed {
			out[index] = item
		}
		return out, true
	case []string:
		out := make([]any, len(typed))
		for index, item := range typed {
			out[index] = item
		}
		return out, true
	}
	return nil, false
}

// asMap returns value as a table.
func asMap(value any) (map[string]any, bool) {
	typed, ok := value.(map[string]any)
	return typed, ok
}

// truthy reports whether value is truthy under Python's rules, which is what
// decides whether a declared "profile" list counts as declared at all.
func truthy(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case bool:
		return typed
	case string:
		return typed != ""
	case int64:
		return typed != 0
	case float64:
		return typed != 0
	case []any:
		return len(typed) > 0
	case []string:
		return len(typed) > 0
	case []map[string]any:
		return len(typed) > 0
	case map[string]any:
		return len(typed) > 0
	default:
		return true
	}
}
