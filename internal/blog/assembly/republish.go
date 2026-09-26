package assembly

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/stricttools/selfdoc/internal/blog/site"
	"github.com/stricttools/selfdoc/internal/blog/sitedirectives"
	"github.com/stricttools/selfdoc/internal/build"
	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/manifest"
	"github.com/stricttools/selfdoc/internal/util"
	"github.com/stricttools/selfdoc/internal/vocabulary"
)

// RepublishCommand is the command that publishes every project on the site
// again in one pass, as a refusal names it.
const RepublishCommand = "selfdoc assembly republish-all"

// RepublishOptions is what one [RepublishAll] takes.
type RepublishOptions struct {
	// HomeDir is the home project's checkout.
	HomeDir string
	// ProjectDirs are the checkouts of every other project on the roster.
	ProjectDirs []string
	// Running is the running selfdoc's own version, which the assembly's
	// deploy workflow must pin at least.
	Running string
}

// RepublishedProject is one project a [RepublishAll] published, or would.
type RepublishedProject struct {
	// Slug is the project's address on the site.
	Slug string
	// Home reports whether it is the home project.
	Home bool
	// Version is the version the publish records.
	Version string
	// Files are the site-relative paths its build produces, sorted.
	Files []string
	// Commit is the assembly commit that carried it, empty in a dry run.
	Commit string
}

// RepublishSummary is what one [RepublishAll] did.
type RepublishSummary struct {
	// Repo is the assembly repository.
	Repo string
	// Pin is the selfdoc version the assembly's deploy workflow pins.
	Pin string
	// Projects are the projects, in the order they were built and published.
	Projects []RepublishedProject
	// Published reports whether anything was published: false in a dry run.
	Published bool
}

// RepublishAll publishes every project on the assembly's roster again, from
// local checkouts, and then sends one deploy request.
//
// It is the one-time pass that replaces every project's manifest on the site
// with one on the current manifest schema, and it refuses before building
// anything when:
//
//   - a checkout is not on the stricttools/ layout, or its manifest is missing
//     or on an older schema (each named, with its fix);
//   - the checkouts do not declare one assembly repository;
//   - the slugs given are not the roster's slugs, exactly, or the home checkout
//     is not the roster's home project;
//   - the assembly's deploy workflow pins a selfdoc older than the running one;
//   - any two projects' vocabularies, or one and selfdoc's baseline, disagree
//     about a word (every conflict listed).
//
// Then it builds every project locally, the home project last, against the
// checkouts' own manifests -- the site's are what this pass replaces -- and
// publishes each through [PublishProjectDocs]. Under a previewing handle the
// builds still run, since what a dry run reports is what they produce, and
// nothing is published.
func RepublishAll(opts RepublishOptions, h *effects.Handle) (*RepublishSummary, error) {
	dirs := append([]string{opts.HomeDir}, opts.ProjectDirs...)
	configs, err := refuseUnpublishable(dirs, h)
	if err != nil {
		return nil, err
	}
	checkouts, err := site.ResolveCheckouts(opts.HomeDir, opts.ProjectDirs)
	if err != nil {
		return nil, err
	}
	repo, err := declaredAssembly(checkouts, configs)
	if err != nil {
		return nil, err
	}
	roster, err := LoadRemoteRoster(h, repo)
	if err != nil {
		return nil, err
	}
	if err := refuseRosterMismatch(checkouts, roster, repo); err != nil {
		return nil, err
	}
	pin, err := DeployedSelfdocPin(h, repo)
	if err != nil {
		return nil, err
	}
	if err := refuseOlderPin(pin, opts.Running, repo); err != nil {
		return nil, err
	}

	manifests, published, err := checkoutManifests(checkouts)
	if err != nil {
		return nil, err
	}
	baseline, err := vocabulary.LoadBaseline()
	if err != nil {
		return nil, err
	}
	if conflicts := vocabulary.SiteConflicts(published, baseline); len(conflicts) > 0 {
		return nil, &vocabulary.ConflictsError{
			Action: "republishing every project to " + repo, Conflicts: conflicts,
		}
	}

	// A dry run reports what the builds produce, so the builds run for real:
	// they write only each checkout's ignored build output.
	buildHandle := h
	if h.Previewing() {
		buildHandle = effects.Unbound()
	}
	summary := &RepublishSummary{Repo: repo, Pin: pin, Published: !h.Previewing()}
	for _, checkout := range checkouts {
		cfg := configs[checkout.SourceDir]
		if err := BuildForPublish(PublishBuildOptions{
			SourceDir: checkout.SourceDir, Config: cfg, Slug: checkout.Slug,
			Home: checkout.Home, HomeSlug: roster.Home, Manifests: manifests,
		}, buildHandle); err != nil {
			return nil, fmt.Errorf("building %s at %s: %w", util.PythonRepr(checkout.Slug), checkout.SourceDir, err)
		}
		outputDir := filepath.Join(checkout.SourceDir, strings.TrimRight(config.OutputRel(cfg), "/"))
		rels, err := site.BuildOutputPaths(outputDir, true)
		if err != nil {
			return nil, err
		}
		var files []string
		for _, siteRel := range site.SplitBuildOutput(rels, checkout.Slug, checkout.Home) {
			files = append(files, siteRel)
		}
		sort.Strings(files)
		summary.Projects = append(summary.Projects, RepublishedProject{
			Slug: checkout.Slug, Home: checkout.Home,
			Version: PublishVersion(checkout.SourceDir, cfg), Files: files,
		})
	}
	if h.Previewing() {
		return summary, nil
	}

	for index, checkout := range checkouts {
		var peers []vocabulary.Published
		for _, other := range published {
			if other.Project != checkout.Slug {
				peers = append(peers, other)
			}
		}
		cfg := configs[checkout.SourceDir]
		result, err := PublishProjectDocs(PublishOptions{
			Repo:         repo,
			Slug:         checkout.Slug,
			OutputDir:    filepath.Join(checkout.SourceDir, strings.TrimRight(config.OutputRel(cfg), "/")),
			Version:      summary.Projects[index].Version,
			ManifestPath: layout.Path(checkout.SourceDir, layout.ManifestRel),
			Home:         checkout.Home,
			SourceDir:    checkout.SourceDir,
			Peers:        peers,
		}, h)
		if err != nil {
			return nil, fmt.Errorf("publishing %s (after %d of %d project(s) were published): %w",
				util.PythonRepr(checkout.Slug), index, len(checkouts), err)
		}
		summary.Projects[index].Commit = result.Push.SHA
	}
	if err := DispatchSharedRebuild(h, repo); err != nil {
		return nil, err
	}
	return summary, nil
}

// refuseUnpublishable reads every checkout's config and manifest and refuses,
// listing every checkout that cannot be published and why, before anything is
// built. It returns the configs by absolute checkout path.
func refuseUnpublishable(dirs []string, h *effects.Handle) (map[string]config.Config, error) {
	configs := map[string]config.Config{}
	var problems []string
	for _, raw := range dirs {
		dir, err := filepath.Abs(raw)
		if err != nil {
			return nil, err
		}
		cfg, err := config.Load(dir)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", dir, err))
			continue
		}
		if cfg == nil {
			problems = append(problems, fmt.Sprintf("%s carries no selfdoc.json, so it is not a project the assembly can serve.", dir))
			continue
		}
		configs[dir] = cfg
		working, err := manifest.Load(layout.Path(dir, layout.ManifestRel))
		switch {
		case err != nil:
			problems = append(problems, fmt.Sprintf("%s: %v", dir, err))
			continue
		case working == nil:
			problems = append(problems, fmt.Sprintf(
				"%s has no %s, so it publishes no vocabulary: generate it with 'selfdoc gen' and commit it.",
				dir, layout.ManifestRel))
			continue
		}
		if _, err := manifest.LoadFromGit(dir, h); err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", dir, err))
		}
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf(
			"Refusing to republish: %d checkout(s) cannot be published, and nothing was built:\n  - %s",
			len(problems), strings.Join(problems, "\n  - "))
	}
	return configs, nil
}

// declaredAssembly is the assembly repository every checkout declares, which
// must be one.
func declaredAssembly(checkouts []site.Checkout, configs map[string]config.Config) (string, error) {
	declared := map[string][]string{}
	var missing []string
	for _, checkout := range checkouts {
		assemblyCfg, _ := configs[checkout.SourceDir]["assembly"].(map[string]any)
		repo := util.PythonStrOrEmpty(assemblyCfg["repo"])
		if repo == "" {
			missing = append(missing, checkout.Slug)
			continue
		}
		declared[repo] = append(declared[repo], checkout.Slug)
	}
	if len(missing) > 0 {
		return "", fmt.Errorf(
			"%s declare(s) no assembly.repo in selfdoc.json, so there is no telling which assembly they publish to. Declare it in each: \"assembly\": {\"repo\": \"<owner>/<repository>\"}.",
			strings.Join(missing, ", "))
	}
	if len(declared) != 1 {
		var lines []string
		for repo, slugs := range declared {
			lines = append(lines, repo+": "+strings.Join(slugs, ", "))
		}
		sort.Strings(lines)
		return "", fmt.Errorf(
			"the checkouts declare more than one assembly.repo, and one pass publishes to one assembly:\n  %s\nRun it once per assembly, naming only that assembly's checkouts.",
			strings.Join(lines, "\n  "))
	}
	for repo := range declared {
		return repo, nil
	}
	return "", nil
}

// refuseRosterMismatch refuses checkouts whose slugs are not the roster's,
// exactly, or whose home is not the roster's home project.
func refuseRosterMismatch(checkouts []site.Checkout, roster *site.Roster, repo string) error {
	given := map[string]bool{}
	homeSlug := ""
	for _, checkout := range checkouts {
		given[checkout.Slug] = true
		if checkout.Home {
			homeSlug = checkout.Slug
		}
	}
	var problems []string
	if roster.Home == "" {
		problems = append(problems, fmt.Sprintf(
			"%s on %s declares no home project, and a republish of the whole site publishes one: declare home = \"<slug>\" in it.",
			site.RosterPath, repo))
	} else if homeSlug != roster.Home {
		problems = append(problems, fmt.Sprintf(
			"--home is the checkout of %s, and %s on %s declares %s as the home project: pass the checkout of %s as --home, and %s as a --repo.",
			util.PythonRepr(homeSlug), site.RosterPath, repo, util.PythonRepr(roster.Home),
			util.PythonRepr(roster.Home), util.PythonRepr(homeSlug)))
	}
	for _, slug := range roster.Slugs() {
		if !given[slug] {
			problems = append(problems, fmt.Sprintf(
				"%s declares %s, and no checkout given declares that slug: add --repo <checkout of %s>.",
				site.RosterPath, util.PythonRepr(slug), slug))
		}
	}
	var extra []string
	for slug := range given {
		if !roster.Has(slug) {
			extra = append(extra, slug)
		}
	}
	sort.Strings(extra)
	for _, slug := range extra {
		problems = append(problems, fmt.Sprintf(
			"a checkout declares %s, which %s on %s does not: drop its --repo, or add a [[project]] block for it to the roster.",
			util.PythonRepr(slug), site.RosterPath, repo))
	}
	if len(problems) > 0 {
		return fmt.Errorf(
			"Refusing to republish: the checkouts given must be the roster's projects, exactly, so every project's manifest on the site is replaced in one pass:\n  - %s",
			strings.Join(problems, "\n  - "))
	}
	return nil
}

// selfdocPinPattern finds the version the deploy workflow's install line
// pins, as the workflow template writes it.
var selfdocPinPattern = regexp.MustCompile(`go install ` + regexp.QuoteMeta(GoModulePath) + `@v(\S+)`)

// DeployedSelfdocPin reads the selfdoc version the assembly's deploy workflow
// installs, from the workflow file on the repository.
func DeployedSelfdocPin(h *effects.Handle, repo string) (string, error) {
	workflow, err := FetchRemoteText(h, repo, site.WorkflowPath, true,
		"read the selfdoc version "+repo+"'s deploy workflow pins")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(workflow) == "" {
		return "", fmt.Errorf(
			"%s has no %s, so no deploy would read what this publishes. Write it with 'selfdoc assembly sync-workflow --pin-selfdoc <version>'.",
			repo, site.WorkflowPath)
	}
	match := selfdocPinPattern.FindStringSubmatch(workflow)
	if match == nil {
		return "", fmt.Errorf(
			"%s on %s carries no 'go install %s@v<version>' line, so the selfdoc its deploy runs is unknown. Regenerate it with 'selfdoc assembly sync-workflow --pin-selfdoc <version>'.",
			site.WorkflowPath, repo, GoModulePath)
	}
	return match[1], nil
}

// refuseOlderPin refuses a deploy workflow pinning a selfdoc older than the
// running one: its deploys would read the manifests this publishes with a
// reader that refuses them.
func refuseOlderPin(pin, running, repo string) error {
	older, err := releaseBefore(pin, running)
	if err != nil {
		return fmt.Errorf("comparing %s's deploy pin with the running selfdoc: %w", repo, err)
	}
	if older {
		return fmt.Errorf(
			"%s on %s pins selfdoc %s, older than this selfdoc %s: its deploys would read the manifests this publishes with a reader that refuses them. Move the pin first with 'selfdoc assembly sync-workflow --pin-selfdoc %s', run in a project whose selfdoc.json declares assembly.repo %s, then run this again.",
			site.WorkflowPath, repo, pin, running, running, repo)
	}
	return nil
}

// releaseBefore reports whether release a is older than release b. Both must
// be plain major.minor.patch versions: a pin is always one, and the running
// selfdoc compared with it is a release too.
func releaseBefore(a, b string) (bool, error) {
	parsedA, err := parseRelease(a)
	if err != nil {
		return false, err
	}
	parsedB, err := parseRelease(b)
	if err != nil {
		return false, err
	}
	for index := range parsedA {
		if parsedA[index] != parsedB[index] {
			return parsedA[index] < parsedB[index], nil
		}
	}
	return false, nil
}

// parseRelease reads a major.minor.patch version.
func parseRelease(version string) ([3]int, error) {
	var parsed [3]int
	parts := strings.Split(strings.TrimPrefix(version, "v"), ".")
	if len(parts) != 3 {
		return parsed, fmt.Errorf("%q is not a major.minor.patch version", version)
	}
	for index, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 {
			return parsed, fmt.Errorf("%q is not a major.minor.patch version", version)
		}
		parsed[index] = number
	}
	return parsed, nil
}

// checkoutManifests reads every checkout's manifest, in the shape the site's
// manifests are read in, and the vocabulary each publishes.
func checkoutManifests(checkouts []site.Checkout) ([]map[string]any, []vocabulary.Published, error) {
	documents := map[string][]byte{}
	for _, checkout := range checkouts {
		raw, err := os.ReadFile(layout.Path(checkout.SourceDir, layout.ManifestRel))
		if err != nil {
			return nil, nil, err
		}
		documents[checkout.Slug+".json"] = raw
	}
	manifests, err := site.ManifestsFromDocuments(documents, "the checkouts")
	if err != nil {
		return nil, nil, err
	}
	var published []vocabulary.Published
	for _, checkout := range checkouts {
		for _, document := range manifests {
			if util.PythonStrOrEmpty(document["slug"]) != checkout.Slug {
				continue
			}
			record, err := manifest.Compat(document, checkout.SourceDir)
			if err != nil {
				return nil, nil, err
			}
			published = append(published, record.Vocabulary.Published(checkout.Slug))
		}
	}
	if len(published) != len(checkouts) {
		return nil, nil, fmt.Errorf(
			"every checkout's manifest must declare the slug its selfdoc.json declares, and %d of %d do: regenerate the others with 'selfdoc gen'",
			len(published), len(checkouts))
	}
	return manifests, published, nil
}

// PublishBuildOptions is what one [BuildForPublish] takes.
type PublishBuildOptions struct {
	// SourceDir is the project's checkout.
	SourceDir string
	// Config is the project's selfdoc.json.
	Config config.Config
	// Slug is the project's address on the site.
	Slug string
	// Home reports whether the project is the site's home project.
	Home bool
	// HomeSlug is the site's home project, whose pages the sibling block and
	// the site name leave out.
	HomeSlug string
	// Manifests are the site's manifests the build reads: the sibling block,
	// the site name, and the home project's site-level directives.
	Manifests []map[string]any
}

// BuildForPublish builds a project's documentation the way the deploy does, on
// its checkout, for a publish that does not clone the assembly: the home
// project through the one build that resolves a site-level directive, every
// other project with the sibling block and the site name its assembled pages
// carry.
func BuildForPublish(opts PublishBuildOptions, h *effects.Handle) error {
	if opts.Home {
		context, err := sitedirectives.HomeContext(opts.SourceDir, opts.Config, opts.Manifests)
		if err != nil {
			return err
		}
		_, err = sitedirectives.BuildHome(opts.SourceDir, opts.Config, context, "", false, h)
		return err
	}
	return BuildSourceProject(BuildOptions{
		SourceDir: opts.SourceDir,
		Scope:     "full",
		Siblings:  build.SiblingsFromManifests(opts.Manifests, opts.HomeSlug, opts.Slug),
		SiteName:  SiteName(opts.Manifests, opts.HomeSlug),
	}, h)
}

// PublishVersion is the version a documentation publish records: the one the
// config declares, else the unversioned marker for a project that declares it
// has none, else the one its package manifests state.
func PublishVersion(dir string, cfg config.Config) string {
	if version, _ := cfg["version"].(string); version != "" {
		return version
	}
	if config.IsUnversioned(cfg) {
		return config.UnversionedVersion
	}
	return util.DetectProjectVersion(dir, "0.0.0")
}

// DispatchSharedRebuild asks the assembly's deploy workflow to regenerate the
// shared cross-project elements from what the repository now holds.
func DispatchSharedRebuild(h *effects.Handle, repo string) error {
	dispatch := SharedOnlyDispatch(repo)
	payload, err := dispatch.PayloadJSON()
	if err != nil {
		return err
	}
	result, err := h.Run(
		[]string{"gh", "api", "--method", "POST", dispatch.Endpoint, "--input", "-"},
		effects.Stdin(payload),
		effects.CaptureOutput(),
		effects.Timeout(30*time.Second),
		effects.Resource("dispatch:"+repo),
		effects.Grant("assembly-dispatch"),
	)
	if err != nil {
		return err
	}
	if !result.Unsettled && result.ExitCode != 0 {
		return fmt.Errorf("Failed to dispatch shared rebuild: %s", strings.TrimSpace(result.StderrString()))
	}
	return nil
}
