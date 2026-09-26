package cli

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/stricttools/selfdoc/internal/blog/assembly"
	"github.com/stricttools/selfdoc/internal/blog/posts"
	"github.com/stricttools/selfdoc/internal/blog/site"
	"github.com/stricttools/selfdoc/internal/build"
	"github.com/stricttools/selfdoc/internal/directives"
	"github.com/stricttools/selfdoc/internal/docs"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/manifest"
	"github.com/stricttools/selfdoc/internal/resolver"
	"github.com/stricttools/selfdoc/internal/revisions"
	"github.com/stricttools/selfdoc/internal/util"
	"github.com/smm-h/strictcli/go/strictcli"
)

// assemblyDispatchGrant is the one grant every command that triggers the
// assembly's rebuild workflow declares.
var assemblyDispatchGrant = strictcli.Grant{
	Name: "assembly-dispatch",
	Reason: "triggers a GitHub Actions workflow on the assembly repository, " +
		"which rebuilds and republishes the live documentation site",
	Kind: strictcli.ProcMutate,
}

func (c *cli) registerPost(parent *strictcli.Group) {
	group := parent.Group("post", "Manage blog posts and chronological content for the documentation site")

	group.Command("new",
		"Scaffold a new blog post markdown file with a date-prefixed filename and frontmatter template containing title, date, slug, tags, draft status, and project metadata. Creates the file in the configured posts directory and exits with an error if the file already exists.",
		c.cmdPostNew,
		strictcli.WithEffect(strictcli.EffectMutating),
		strictcli.WithFlags(
			strictcli.StringFlag("title", "Title for the new blog post, used in frontmatter and filename generation", strictcli.Required()),
		),
	)

	group.Command("list",
		"List all discovered blog posts with date, title, slug, and draft status. Scans the configured posts directory for markdown files with frontmatter, parses their metadata, and prints a formatted summary showing each post's publication date, title, slug identifier, and whether it is marked as a draft.",
		c.cmdPostList,
		strictcli.WithEffect(strictcli.EffectReadOnly),
	)

	group.Command("generate",
		"Generate a blog post markdown file from structured release metadata. Takes version, bump type, description, changelog, and registry URLs as inputs, produces a frontmatter-bearing post with title, date, tags, and body content, and updates the project manifest with the new post entry.",
		c.cmdPostGenerate,
		strictcli.WithEffect(strictcli.EffectMutating),
		strictcli.WithFlags(
			strictcli.BoolFlag("from-release", "Generate the post from structured release metadata rather than freeform content. Structured metadata is the only shape this command generates, so --no-from-release is refused; state --from-release to say what the post is built from", strictcli.Required()),
			strictcli.StringFlag("version", "The released version number to feature in the generated blog post title and metadata", strictcli.Required()),
			strictcli.StringFlag("prev-version", "Previous version number, used to show what version this release upgrades from", strictcli.Optional()),
			strictcli.StringFlag("bump-type", "Semver bump type (patch, minor, or major) included in the post frontmatter", strictcli.Optional()),
			strictcli.StringFlag("description", "Short release description text included as the post summary paragraph", strictcli.Optional()),
			strictcli.StringFlag("context", "Additional context explaining the rationale for this release, included in generated blog posts", strictcli.Optional()),
			strictcli.StringFlag("changelog-file", "Path to a markdown file whose contents are embedded as the changelog section of the post", strictcli.Optional()),
			strictcli.StringFlag("body-file", "Path to a file containing user-written prose to include as the main post body content", strictcli.Optional()),
			strictcli.StringFlag("project-name", "Human-readable project name used in the blog post title and frontmatter metadata", strictcli.Optional()),
			strictcli.StringFlag("release-url", "Full URL to the GitHub release page, linked from the generated blog post", strictcli.Optional()),
			strictcli.StringFlag("registry-url", "Package registry URL such as PyPI or npm page, can be specified multiple times",
				strictcli.Repeatable(), strictcli.Unique(false), strictcli.Default([]any{})),
		),
	)

	group.Command("publish",
		"Publish non-draft blog posts to the documentation assembly. Builds posts locally, pushes built HTML and manifest to the assembly repo via the Git Data API, then dispatches a shared-only workflow to regenerate cross-project elements.",
		c.cmdPostPublish,
		strictcli.WithEffect(strictcli.EffectMutating),
		// Consequential: this is the moment locally-authored, previously
		// private post content becomes publicly readable. Unlike `assembly
		// push` and `assembly rebuild`, which re-derive already-public docs
		// from an already-public tag, this one publishes something new, and a
		// post published by mistake cannot be unpublished from the reader's
		// side.
		strictcli.WithConsequential(),
		strictcli.WithGrants(assemblyDispatchGrant),
	)
}

func (c *cli) cmdPostNew(ctx *strictcli.Context, kwargs map[string]any) strictcli.Outcome {
	title := strictcli.Get[string](kwargs, "title")
	handle := effects.FromContext(ctx)

	// Presence is the framework's now; the VALUE is still this command's to
	// judge, and an empty title names a file `<date>-.md` with no slug in it.
	if strings.TrimSpace(title) == "" {
		return c.failf("--title must be a non-empty title.")
	}

	cfg, outcome, ok := c.requireConfig()
	if !ok {
		return outcome
	}

	postsDirRel := strings.TrimRight(postsDirOf(cfg), "/")
	slug := manifest.ToKebab(title)
	today := time.Now().Format("2006-01-02")
	filename := today + "-" + slug + ".md"
	relPath := filepath.Join(postsDirRel, filename)
	fullPath := filepath.Join(c.dir(), relPath)

	if info, err := os.Stat(fullPath); err == nil && !info.IsDir() {
		return c.failf("Error: Post file already exists: %s", relPath)
	}

	if err := layout.EnsureDir(handle, c.dir(), postsDirRel); err != nil {
		return c.fail(err)
	}

	frontmatter, err := util.RenderFrontmatter([]util.FrontmatterField{
		{Key: "title", Value: title},
		{Key: "date", Value: util.FrontmatterDate(today)},
		{Key: "slug", Value: slug},
		{Key: "tags", Value: []string{}},
		{Key: "draft", Value: true},
		{Key: "directives", Value: false},
	})
	if err != nil {
		return c.fail(err)
	}
	content := frontmatter + "\n"

	if err := handle.Write(fullPath, []byte(content), effects.ModeDefault); err != nil {
		return c.fail(err)
	}

	c.printf("Created post: %s\n", relPath)
	return strictcli.Exit(0)
}

func (c *cli) cmdPostList(ctx *strictcli.Context, kwargs map[string]any) strictcli.Outcome {
	handle := effects.FromContext(ctx)

	cfg, outcome, ok := c.requireConfig()
	if !ok {
		return outcome
	}

	postsDir := filepath.Join(c.dir(), strings.TrimRight(postsDirOf(cfg), "/"))
	discovered, err := posts.Discover(postsDir, "", handle)
	if err != nil {
		return c.fail(err)
	}

	if len(discovered) == 0 {
		c.println("No posts found.")
		return strictcli.Exit(0)
	}

	for _, post := range discovered {
		draftMarker := ""
		if post.Draft {
			draftMarker = "  [DRAFT]"
		}
		c.printf("%s  %s  (%s)%s\n", post.Date, post.Title, post.Slug, draftMarker)
	}

	c.printf("\n%d post(s) found.\n", len(discovered))
	return strictcli.Exit(0)
}

func (c *cli) cmdPostGenerate(ctx *strictcli.Context, kwargs map[string]any) strictcli.Outcome {
	fromRelease := strictcli.Get[bool](kwargs, "from_release")
	version := strictcli.Get[string](kwargs, "version")
	prevVersion := optString(kwargs, "prev_version")
	bumpType := optString(kwargs, "bump_type")
	changelogFile := optString(kwargs, "changelog_file")
	bodyFile := optString(kwargs, "body_file")
	name := optString(kwargs, "project_name")
	releaseURL := optString(kwargs, "release_url")
	registryURLs := stringList(kwargs, "registry_url")
	handle := effects.FromContext(ctx)

	// Presence is declared, so the framework demands a choice here; the
	// command still owns which choice it can honour, and freeform generation
	// was never implemented.
	if !fromRelease {
		return c.failf("Error: --no-from-release names a mode this command does not " +
			"have; pass --from-release.")
	}

	if strings.TrimSpace(version) == "" {
		return c.failf("Error: --version must be a non-empty version.")
	}

	cfg, outcome, ok := c.requireConfig()
	if !ok {
		return outcome
	}

	postsDirRel := strings.TrimRight(postsDirOf(cfg), "/")

	changelogContent := ""
	if changelogFile != "" {
		data, err := os.ReadFile(changelogFile)
		if err != nil {
			return c.fail(err)
		}
		changelogContent = strings.TrimSpace(string(data))
	}

	bodyContent := ""
	if bodyFile != "" {
		data, err := os.ReadFile(bodyFile)
		if err != nil {
			return c.fail(err)
		}
		bodyContent = strings.TrimSpace(string(data))
	}

	today := time.Now().Format("2006-01-02")
	slug := "release-v" + version
	filename := today + "-" + slug + ".md"

	title := "Release v" + version
	if name != "" {
		title = name + " v" + version
	}

	fields := []util.FrontmatterField{
		{Key: "title", Value: title},
		{Key: "date", Value: util.FrontmatterDate(today)},
		{Key: "slug", Value: slug},
		{Key: "draft", Value: false},
		// A generated release post is prose plus a changelog; nothing in it
		// is meant to be resolved. The declaration is required on every post,
		// so the scaffold states it rather than leaving the author a file its
		// own discovery would refuse.
		{Key: "directives", Value: false},
		{Key: "version", Value: version},
	}
	if prevVersion != "" {
		fields = append(fields, util.FrontmatterField{Key: "prev_version", Value: prevVersion})
	}
	if bumpType != "" {
		fields = append(fields, util.FrontmatterField{Key: "bump_type", Value: bumpType})
	}
	if releaseURL != "" {
		fields = append(fields, util.FrontmatterField{Key: "release_url", Value: releaseURL})
	}
	if len(registryURLs) > 0 {
		fields = append(fields, util.FrontmatterField{Key: "registry_urls", Value: registryURLs})
	}
	fields = append(fields,
		util.FrontmatterField{Key: "tags", Value: []string{"release", "v" + version}},
	)
	frontmatter, err := util.RenderFrontmatter(fields)
	if err != nil {
		return c.fail(err)
	}

	var bodyParts []string
	if bodyContent != "" {
		bodyParts = append(bodyParts, bodyContent)
	}
	if changelogContent != "" {
		bodyParts = append(bodyParts, "## Changelog\n\n"+changelogContent)
	}
	if len(bodyParts) == 0 {
		bodyParts = append(bodyParts, "Version "+version+" has been released.")
	}

	fullContent := frontmatter + "\n" + strings.Join(bodyParts, "\n\n") + "\n"

	// Under --dry-run the writes below are recorded by the effects chokepoint
	// and rendered in the would-do log, which replaces the command's old
	// local --dry-run (a reserved framework name now).
	if err := layout.EnsureDir(handle, c.dir(), postsDirRel); err != nil {
		return c.fail(err)
	}
	relPath := filepath.Join(postsDirRel, filename)
	if err := handle.AtomicWrite(filepath.Join(c.dir(), relPath),
		[]byte(fullContent), effects.ModeDefault); err != nil {
		return c.fail(err)
	}
	c.printf("Created post: %s\n", relPath)

	manifestRel := layout.ManifestRel
	manifestPath := filepath.Join(c.dir(), manifestRel)
	existing, err := manifest.Load(manifestPath)
	if err != nil {
		return c.fail(err)
	}

	entry := jsonObject{
		{"path", filename},
		{"title", title},
		{"date", today},
		{"slug", slug},
		{"tags", []any{"release", "v" + version}},
	}

	if existing != nil {
		// The document is patched rather than regenerated, so every field it
		// carries that this command has no opinion about is written back
		// exactly as it stood -- including its key order.
		raw, err := os.ReadFile(manifestPath)
		if err != nil {
			return c.fail(err)
		}
		decoded, err := decodeOrderedJSON(raw)
		if err != nil {
			return c.fail(err)
		}
		document, ok := decoded.(jsonObject)
		if !ok {
			return c.failf("Error: %s is not a JSON object.", manifestRel)
		}
		postsList := []any{}
		if value, ok := document.get("posts"); ok {
			if items, ok := value.([]any); ok {
				postsList = items
			}
		}
		postsList = append(postsList, entry)
		document = document.set("posts", postsList)
		document = document.set("version", version)

		if err := handle.AtomicWrite(manifestPath,
			[]byte(encodeJSON(document, 0)+"\n"), effects.ModeDefault); err != nil {
			return c.fail(err)
		}
		c.printf("Updated manifest: %s\n", manifestRel)
		return strictcli.Exit(0)
	}

	// No existing manifest: generate one fresh. The home project's pages
	// carry site-level markers, so the resolution that enumerates them needs
	// the same registration the check makes.
	allDocs, err := docs.ResolveAll(
		c.withSiteDirectives(cfg, handle), "", c.dir(), nil, handle,
	)
	if err != nil {
		return c.fail(err)
	}
	if _, err := manifest.Generate(cfg, c.dir(), docs.ManifestDocs(allDocs),
		[]manifest.Post{{
			Path:  filename,
			Title: title,
			Date:  today,
			Slug:  slug,
			Tags:  []string{"release", "v" + version},
		}}, manifest.DefaultOutputName, handle); err != nil {
		return c.fail(err)
	}
	c.printf("Created manifest: %s\n", manifestRel)
	return strictcli.Exit(0)
}

func (c *cli) cmdPostPublish(ctx *strictcli.Context, kwargs map[string]any) strictcli.Outcome {
	handle := effects.FromContext(ctx)
	dir := c.dir()

	cfg, outcome, ok := c.requireConfig()
	if !ok {
		return outcome
	}

	repo := configString(cfg, "assembly", "repo")
	if repo == "" {
		return c.failf("Error: assembly.repo not configured in selfdoc.json.")
	}
	slug := configString(cfg, "topology", "slug")
	if slug == "" {
		return c.failf("Error: topology.slug not configured in selfdoc.json.")
	}

	postsDir := filepath.Join(dir, strings.TrimRight(postsDirOf(cfg), "/"))
	discovered, err := posts.Discover(postsDir, "", handle)
	if err != nil {
		return c.fail(err)
	}
	published := make([]posts.Post, 0, len(discovered))
	for _, post := range discovered {
		if !post.Draft {
			published = append(published, post)
		}
	}
	if len(published) == 0 {
		c.println("No non-draft posts to publish.")
		return strictcli.Exit(0)
	}

	// Record revisions for posts whose body content changed.
	revisionsRel := layout.RevisionsRel
	revisionsPath := filepath.Join(dir, revisionsRel)
	for _, post := range published {
		summary := ""
		if info, err := os.Stat(revisionsPath); err != nil || info.IsDir() {
			summary = "Initial publish"
		}
		if _, err := revisions.RecordRevision(handle, dir, post.Slug, post.Content, summary); err != nil {
			return c.fail(err)
		}
	}

	outputRel := strings.TrimRight(outputDirOf(cfg), "/")
	outputDir := filepath.Join(dir, outputRel)
	docsDirName := strings.TrimRight(docsDirOf(cfg), "/")
	// A local posts build is standalone: it has no assembled site around it,
	// so there are no siblings to link and no site name to end a title with.
	written, err := build.BuildPostsOnly(dir, cfg, outputDir, docsDirName,
		filepath.Join(dir, docsDirName), false, nil, "", handle)
	if err != nil {
		return c.fail(err)
	}

	// Read built HTML files and map to assembly paths. A post is site-level
	// -- `blog/<post-slug>/`, under no project slug -- and the listing page
	// the build renders for the project's own standalone site is dropped: the
	// assembled site's blog index lists every project's posts and is written
	// by the shared-only rebuild dispatched below.
	buildRels := make([]string, 0, len(written))
	for absPath := range written {
		rel, err := filepath.Rel(outputDir, absPath)
		if err != nil {
			return c.fail(err)
		}
		buildRels = append(buildRels, filepath.ToSlash(rel))
	}
	sort.Strings(buildRels)

	files := map[string][]byte{}
	var produced []string
	for buildRel, siteRel := range site.SplitBuildOutput(buildRels, slug, false) {
		data, err := os.ReadFile(filepath.Join(outputDir, filepath.FromSlash(buildRel)))
		if err != nil {
			return c.fail(err)
		}
		files["site/"+siteRel] = data
		produced = append(produced, siteRel)
	}
	sort.Strings(produced)

	postManifestPath := layout.Path(dir, layout.PostManifestRel)
	if info, err := os.Stat(postManifestPath); err == nil && !info.IsDir() {
		data, err := os.ReadFile(postManifestPath)
		if err != nil {
			return c.fail(err)
		}
		files["manifests/"+slug+"-posts.json"] = data
	}

	if info, err := os.Stat(revisionsPath); err == nil && !info.IsDir() {
		data, err := os.ReadFile(revisionsPath)
		if err != nil {
			return c.fail(err)
		}
		files["manifests/"+slug+"-revisions.json"] = data
	}

	// Record the posts this publish owns, in the same commit that carries
	// them: an unrecorded post is unclaimed, so nothing accounts for it,
	// another project's publish may overwrite it, and retirement leaves it
	// behind on the blog.
	var deletePaths []string
	if len(produced) > 0 {
		// The site-level blog is one namespace shared by every project, so a
		// post slug another project already claims is refused before anything
		// is written -- the same refusal the integrate graft makes, against
		// the records on the assembly instead of a local clone.
		roster, err := assembly.LoadRemoteRoster(handle, repo)
		if err != nil {
			return c.fail(err)
		}
		var others []string
		for _, declared := range roster.Slugs() {
			if declared != slug {
				others = append(others, declared)
			}
		}
		claims, err := assembly.RemotePostClaims(handle, repo, slug, others)
		if err != nil {
			return c.fail(err)
		}
		if err := site.RefuseForeignPostOverwrite(slug, produced, claims); err != nil {
			return c.fail(err)
		}
		deletePaths, err = site.StagePublishedRecord(
			assembly.RemoteTextFetcher(handle), repo, slug, "posts", produced, files,
		)
		if err != nil {
			return c.fail(err)
		}
	} else {
		// The same protection the posts-scope integrate has: a build that
		// emitted no post pages is not an instruction to unpublish the posts
		// already on the site, so the record is left exactly as it is.
		c.eprintf("posts publish for %q: the build produced no post pages, "+
			"so nothing was claimed and nothing was removed. Posts already "+
			"published stay.\n", slug)
	}

	if _, err := assembly.PushFilesToRepo(handle, repo, files, "posts: "+slug,
		assemblyDefaultBranch, deletePaths); err != nil {
		return c.fail(err)
	}

	if outcome, ok := c.dispatchSharedRebuild(handle, repo); !ok {
		return outcome
	}

	// Archive resolved markdown to the posts repo if configured.
	if postsRepo := configString(cfg, "posts", "repo"); postsRepo != "" {
		projectResolver, err := resolver.MakeResolver(cfg, dir, handle)
		if err != nil {
			return c.fail(err)
		}
		validNames, err := docs.ValidNames(cfg)
		if err != nil {
			return c.fail(err)
		}
		postFiles := map[string][]byte{}
		for _, post := range published {
			resolved, err := directives.ResolveDirectives(post.Content, projectResolver.Resolve, validNames)
			if err != nil {
				return c.fail(err)
			}
			postFiles[slug+"/"+post.Path] = []byte(resolved)
		}
		if _, err := assembly.PushFilesToRepo(handle, postsRepo, postFiles, "posts: "+slug,
			assemblyDefaultBranch, nil); err != nil {
			return c.fail(err)
		}
		c.printf("Archived %d post(s) to %s\n", len(published), postsRepo)
	}

	c.printf("Published %d post(s) to assembly. Shared elements will regenerate.\n", len(published))
	return strictcli.Exit(0)
}

// dispatchSharedRebuild fires the shared-only rebuild that regenerates the
// assembly's cross-project elements, which three commands do identically.
func (c *cli) dispatchSharedRebuild(handle *effects.Handle, repo string) (strictcli.Outcome, bool) {
	if err := assembly.DispatchSharedRebuild(handle, repo); err != nil {
		return c.failf("Error: %v", err), false
	}
	return strictcli.Exit(0), true
}
