package cli

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/stricttools/selfdoc/internal/blog/assembly"
	"github.com/stricttools/selfdoc/internal/blog/preview"
	"github.com/stricttools/selfdoc/internal/blog/serving"
	"github.com/stricttools/selfdoc/internal/blog/site"
	"github.com/stricttools/selfdoc/internal/blog/verify"
	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/manifest"
	"github.com/stricttools/selfdoc/internal/util"
	"github.com/smm-h/strictcli/go/strictcli"
)

// assemblyDefaultBranch is the assembly repository branch every command
// commits to when the caller names none. A command that resolves to an empty
// branch does not fall back to the repository's default -- it renders
// "/git/ref/heads/", which the GitHub API answers 404 to.
const assemblyDefaultBranch = "main"

// assemblyCommitGrant is the grant every command that writes a commit on the
// assembly repository declares.
var assemblyCommitGrant = strictcli.Grant{
	Name: "assembly-commit",
	Reason: "pushes a commit to the assembly repository's deploy branch, " +
		"which is the content the live documentation site serves",
	Kind: strictcli.ProcMutate,
}

// selfdocPin is the selfdoc version a generated deploy workflow installs: the
// one --pin-selfdoc names, or -- when the flag is absent -- the running
// binary's own version, which the binary hands in through Options.Version.
//
// An empty answer is not repaired here. It reaches the resolver, which refuses
// it by name, so a caller that supplied no version at all is told so rather
// than having one invented.
func (c *cli) selfdocPin(explicit string) string {
	if explicit != "" {
		return explicit
	}
	return c.opts.Version
}

func (c *cli) registerAssembly() {
	group := c.app.Group("assembly", "Manage the unified multi-project documentation assembly and deployment")

	group.Command("init",
		"Create and initialize the assembly GitHub repository with workflow and configuration files. Creates a private GitHub repo, pushes initial files via the Contents API, creates a Cloudflare Pages project if credentials are available, and sets GitHub secrets for deployment authentication.",
		c.cmdAssemblyInit,
		strictcli.WithEffect(strictcli.EffectMutating),
		// Consequential: every one of its three effects creates a NAMED
		// external resource that rerunning cannot un-create -- a GitHub
		// repository under the configured owner, a Cloudflare Pages project
		// that claims its *.pages.dev subdomain, and deployment credentials
		// written into that repo's Actions secrets. This is the
		// highest-stakes command in the tree.
		strictcli.WithConsequential(),
		strictcli.WithGrants(
			strictcli.Grant{
				Name: "create-repo",
				Reason: "creates a new private GitHub repository under the configured " +
					"owner; repository creation is not undone by rerunning the command",
				Kind: strictcli.ProcMutate,
			},
			strictcli.Grant{
				Name: "create-pages-project",
				Reason: "creates a Cloudflare Pages project on the configured account, " +
					"claiming its *.pages.dev subdomain",
				Kind: strictcli.ProcMutate,
			},
			strictcli.Grant{
				Name: "set-secret",
				Reason: "writes Cloudflare deployment credentials into the assembly " +
					"repository's GitHub Actions secrets",
				Kind: strictcli.ProcMutate,
			},
		),
	)

	group.Command("push",
		"Dispatch a GitHub Actions workflow to rebuild this project in the documentation assembly. Detects the source repository, resolves the latest git tag as the version reference, and sends a repository dispatch event to the assembly repo with the project slug, version, and commit SHA.",
		c.cmdAssemblyPush,
		strictcli.WithEffect(strictcli.EffectMutating),
		// Deliberately NOT consequential, though its grant escapes the
		// process: the dispatch re-derives already-public documentation from
		// an already-public git tag. Nothing new becomes public, nothing
		// published is destroyed, and rerunning converges on the same site.
		// It is also the routine post-release refresh, so a prompt here would
		// be the reflex the confirm protocol exists to avoid.
		strictcli.WithGrants(assemblyDispatchGrant),
	)

	group.Command("status",
		"Show the status of recent assembly build workflow runs on GitHub. Queries the assembly repository for recent workflow runs using the GitHub CLI and displays their status, conclusion, and timing information for monitoring deployment progress.",
		c.cmdAssemblyStatus,
		strictcli.WithEffect(strictcli.EffectReadOnly),
	)

	group.Command("rebuild",
		"Dispatch rebuild workflows for every project registered in the assembly. Fetches the projects.json manifest from the assembly repository, then sends a separate GitHub Actions repository dispatch event for each registered project to trigger a full documentation rebuild.",
		c.cmdAssemblyRebuild,
		strictcli.WithEffect(strictcli.EffectMutating),
		// Deliberately NOT consequential, for the same reason as `assembly
		// push` and despite the wider reach: it re-derives every registered
		// project's already-public docs from their already-public tags. The
		// scale is larger; the character of the change is not.
		strictcli.WithGrants(assemblyDispatchGrant),
	)

	group.Command("retire",
		"Retire a project from the unified assembly: remove its [[project]] block from the roster and, in the same commit, delete its whole site subtree, all of its manifests and its membership record, then dispatch a shared-only rebuild so the listing, feed, sitemap and search index stop naming it.",
		c.cmdAssemblyRetire,
		strictcli.WithEffect(strictcli.EffectMutating),
		// Consequential: it deletes a project's published section from the
		// live site. Nothing else in the tree removes public content, and
		// rerunning cannot restore it -- the pages are gone from the branch
		// the site serves.
		strictcli.WithConsequential(),
		strictcli.WithGrants(assemblyCommitGrant, assemblyDispatchGrant),
		strictcli.WithFlags(
			strictcli.StringFlag("slug", "Slug of the project to retire; it is removed from the roster and every path it owns in the assembly is deleted", strictcli.Required()),
		),
	)

	group.Command("redirects",
		"Generate a Cloudflare Pages _redirects file for this project that redirects standalone documentation URLs to the corresponding paths on the unified assembly site. Requires a project slug and assembly base URL as inputs, prints the redirect rules to stdout.",
		c.cmdAssemblyRedirects,
		strictcli.WithEffect(strictcli.EffectReadOnly),
		strictcli.WithFlags(
			strictcli.StringFlag("slug", "Project slug used as the URL path segment in the assembly site structure", strictcli.Required()),
			strictcli.StringFlag("docs-base", "Base URL of the assembly documentation site used for generating redirect targets", strictcli.Required()),
		),
	)

	group.Command("generate-shared",
		"Generate the shared cross-project elements for the assembled documentation site. Reads per-project manifest JSON files, merges post overlays, and produces a homepage, blog index, navigation JSON, RSS feed, XML sitemap, robots.txt, a site-wide llms.txt linking to each project's own, a root 404 page and a security headers file in the site output directory. It also deletes the redirect worker a deploy made before the worker was retired left at the site root.",
		c.cmdAssemblyGenerateShared,
		strictcli.WithEffect(strictcli.EffectMutating),
		strictcli.WithFlags(
			strictcli.StringFlag("site-dir", "Path to the combined site output directory where shared HTML files are written", strictcli.Required()),
			strictcli.StringFlag("manifests-dir", "Path to the directory containing per-project manifest JSON files for the assembly", strictcli.Required()),
			strictcli.StringFlag("docs-base", "Base URL the Atom feed's entries are written against. Only the feed reads it: every entry there is an absolute URL by protocol. Nothing a reader clicks does -- the generated listing, the blog index and the 404 address the site relative to their own page, so they resolve under any mount. The sitemap does not read it either: it is generated from --canonical-base whatever this says.", strictcli.Optional()),
			strictcli.StringFlag("canonical-base", "Absolute canonical base URL of the assembly site, from topology.docs_base (e.g. 'https://docs.smmh.dev'). Required: it is the one hostname that serves content, it is the base of every sitemap entry, and it targets the rel=canonical links on the homepage and blog index, so it cannot be root-relative like --docs-base.", strictcli.Required()),
			strictcli.StringFlag("home-slug", "The roster's home project: the one project served at the site root. Its pages are left out of the generated listing and out of nav, and every site-level directive region it emitted is re-rendered from the current manifests. Omitted means the tree carries no home project (which the deploy path never does -- the roster requires one).", strictcli.Optional()),
		),
	)

	group.Command("integrate",
		"Integrate one dispatched project into the assembly repository checkout and push the result. Builds the cloned source project, replaces its subtree under site/, refreshes its manifest and membership record, regenerates the shared cross-project elements, rebuilds the search index, then commits and pushes with a re-sync retry loop so concurrent deploys converge instead of clobbering each other. A full-scope deploy first checks the vocabulary the project's manifest records against every other project's manifest on the site and selfdoc's built-in baseline, and refuses before touching the checkout when one project's rejected pattern covers a word another accepts, naming both projects, the word, the pattern and the fix. This is the whole body of the generated deploy workflow.",
		c.cmdAssemblyIntegrate,
		strictcli.WithEffect(strictcli.EffectMutating),
		// Deliberately NOT consequential: it runs unattended inside the
		// assembly repo's own CI, re-deriving already-public docs from an
		// already-public tag. A prompt here would hang the deploy forever.
		//
		// It cannot honestly preview itself, and says so at parse time: every
		// step after the first reads what the step before it wrote, so a
		// recorded run has nothing to hand the steps that follow.
		strictcli.WithDryRunUnsupported(
			"'assembly integrate' cannot be previewed: every step after the "+
				"first reads what the step before it wrote -- the shared generator "+
				"reads the manifests the graft just copied, the search index reads "+
				"the pages it just wrote, and the commit reads the tree all of them "+
				"produced. A recorded run writes none of that, so the preview would "+
				"stop at the first effect and misrepresent everything after it. To "+
				"see what an integration would produce, use 'assembly preview'."),
		strictcli.WithGrants(assemblyCommitGrant),
		strictcli.WithFlags(
			strictcli.StringFlag("slug", "Project slug being integrated; the site subtree is site/<slug>/. Required unless --scope is 'shared-only'.", strictcli.Optional()),
			strictcli.StringFlag("version", "Version of the project being integrated, recorded in projects.json and the commit message", strictcli.Optional()),
			strictcli.StringFlag("ref", "Git ref (tag) the source project was cloned at, recorded in projects.json", strictcli.Optional()),
			strictcli.StringFlag("source-repo", "Source project repository (owner/name), recorded in projects.json", strictcli.Optional()),
			strictcli.StringFlag("scope", "What this dispatch replaces: 'full' (the whole project subtree plus this project's posts), 'posts' (only this project's posts, at the site-level site/blog/<post-slug>/), or 'shared-only' (no project files, just the cross-project elements). Omitted means 'full'.", strictcli.Optional()),
			strictcli.StringFlag("canonical-base", "Absolute canonical base URL of the assembly site, from topology.docs_base. Required: it is the base of every sitemap entry and it targets the rel=canonical links.", strictcli.Required()),
			strictcli.StringFlag("assembly-dir", "Path to the assembly repository checkout being updated. Omitted, the current directory is used", strictcli.Optional()),
			strictcli.StringFlag("source-dir", "Path to the cloned source project. Omitted, <assembly-dir>/source/<slug> is used, where the deploy workflow clones it.", strictcli.Optional()),
			strictcli.StringFlag("branch", "Assembly repository branch the deploy commits and pushes to. Omitted, 'main' is used", strictcli.Optional()),
			strictcli.IntFlag("attempts", "How many times to re-sync with the remote and retry the push before failing. Omitted, 3 attempts are made", strictcli.Optional()),
		),
	)

	group.Command("verify",
		"Assert every property a built assembly tree has to have before it is deployed: that the roster, the site subtrees and the manifests name the same projects, that each manifest's pages and posts were actually emitted, that the shared cross-project artifacts exist and parse, that every internal reference, sitemap entry, feed link and cross-project link resolves, that every page has a title and a canonical, and that no unresolved directive or per-project routing file survived. The deploy runs this itself before it pushes; this command is how you run the same assertions by hand against a checkout.",
		c.cmdAssemblyVerify,
		strictcli.WithEffect(strictcli.EffectReadOnly),
		strictcli.WithFlags(
			strictcli.StringFlag("assembly-dir", "Path to the assembly repository checkout to verify", strictcli.Default(".")),
			strictcli.StringFlag("canonical-base", "Absolute canonical base URL of the assembly site, from topology.docs_base. Required: it is what tells this site's absolute URLs from everybody else's, and without it half the assertions would pass by not looking.", strictcli.Required()),
		),
	)

	group.Command("republish-all",
		"Publish every project on the assembly's roster again, from local checkouts, in one pass: the one-time step that replaces every project's manifest on the site with one on manifest schema_version "+strconv.Itoa(manifest.SchemaVersion)+", which records each project's vocabulary. Before building anything it refuses a checkout not on the "+layout.Root+"/ layout or whose manifest is missing or on an older schema (naming 'selfdoc layout migrate'), checkouts that declare no single assembly.repo, slugs that are not the roster's exactly or a --home that is not the roster's home project, an assembly deploy workflow pinning a selfdoc older than this one (naming 'selfdoc assembly sync-workflow --pin-selfdoc <version>'), and any two projects' vocabularies, or one and selfdoc's built-in baseline, that disagree about a word, listing every conflict. Then it builds every project locally against the checkouts' own manifests, the home project last, publishes each the way 'blog publish-docs' does (one assembly commit per project, which also converts the project's post overlay on the site, manifests/<slug>-posts.json, when an older selfdoc wrote it, keeping its posts and adding the vocabulary of the checkout's manifest), and sends one shared-only deploy request. --dry-run runs the checks and the local builds, which write only each checkout's build output, and prints what it would publish without publishing anything.",
		c.cmdAssemblyRepublishAll,
		strictcli.WithEffect(strictcli.EffectMutating),
		// Consequential for the reason `blog publish-docs` is, for every
		// project at once: locally built content becomes publicly readable,
		// and each publish deletes the pages its project no longer builds.
		strictcli.WithConsequential(),
		strictcli.WithGrants(assemblyDispatchGrant),
		strictcli.WithFlags(
			strictcli.StringFlag("repo", "Path to the checkout of one project on the roster other than the home project. Repeat once per project: together with --home, the checkouts must declare the roster's slugs exactly.",
				strictcli.Repeatable(), strictcli.Unique(true), strictcli.Default([]any{})),
			strictcli.StringFlag("home", "Path to the checkout of the roster's home project, the one served at the site root. Required: a republish of the whole site publishes its front page too.", strictcli.Required()),
		),
	)

	group.Command("preview",
		"Assemble every named local checkout into a preview tree and serve it on loopback. Builds each project with the toolchain running this command, grafts the output exactly as the deploy does -- the home project at the site root, everybody else under their slug -- writes the roster, membership record and manifests the assembly keeps, generates the shared cross-project files and the site chrome, rebuilds the search index, runs the real pre-deploy verification and prints its report, then serves the result with a working 404. Nothing leaves the machine and nothing is published: this is the look-before-you-ship step.",
		c.cmdAssemblyPreview,
		strictcli.WithEffect(strictcli.EffectMutating),
		// Not consequential: everything it writes is a local build tree at a
		// path the caller named, and nothing reaches the world.
		//
		// It cannot preview itself, and says so at parse time. The command's
		// whole output is a tree on disk plus a server over it; recording the
		// writes instead of performing them would leave nothing to look at,
		// and every step after the first reads what the step before it wrote.
		strictcli.WithDryRunUnsupported(
			"'assembly preview' exists to produce output you look at: a built "+
				"site tree and a server over it. A recorded run would write no tree, "+
				"and every step after the build reads what the step before it wrote "+
				"-- the graft reads the build, the shared generator reads the grafted "+
				"manifests, the index and the verification read the pages. Run it "+
				"without --dry-run; it publishes nothing."),
		strictcli.WithFlags(
			strictcli.StringFlag("repo", "Path to a project checkout to include, served under the slug its selfdoc.json declares. Repeat once per project. The home project is named by --home instead and must not be repeated here.",
				strictcli.Repeatable(), strictcli.Unique(true), strictcli.Default([]any{})),
			strictcli.StringFlag("home", "Path to the home project's checkout: the one project served at the site root rather than under a slug. Required -- a site needs a front page, and there is no default.", strictcli.Required()),
			strictcli.StringFlag("out", "Directory the preview tree is written to. Required. Refused when it sits inside a git working tree at a path git does not ignore, because a generated site dropped into a checkout is untracked noise in every session sharing it.", strictcli.Required()),
			strictcli.IntFlag("port", "Port to bind on 127.0.0.1. Required and has no default: which port a long-running local server occupies is a decision the caller states rather than inherits.", strictcli.Required()),
			strictcli.StringFlag("canonical-base", "Absolute canonical base URL of the assembly site, from topology.docs_base (e.g. 'https://smmh.dev'). Required, and it is the DEPLOYED base rather than the loopback one: the preview shows the pages with the canonicals, sitemap entries and cross-project links they would ship with, and verifies those.", strictcli.Required()),
			strictcli.BoolFlag("build", "Whether to build each checkout before grafting it. Required with no default: --build is the honest preview of what would ship, --no-build re-assembles whatever each checkout already has in its build output directory, which is what a second look after one edit wants and the only way to iterate without rebuilding every project. Choosing is the point -- a preview of a stale build tree is a preview of nothing in particular.", strictcli.Required()),
			strictcli.StringFlag("theme", "Build every checkout under this theme instead of the one its selfdoc.json declares, for this preview only. Omitted, every project stays on its configured theme, which is what a deploy does. This exists to judge a theme on the real pages: the same site, every project flipped at once, without editing a config anywhere. Validated against the theme registry, and refused with --no-build, because a theme is baked into build output and re-grafting an existing tree cannot restyle it.", strictcli.Optional()),
		),
	)

	group.Command("sync-workflow",
		"Regenerate the assembly repository's deploy workflow from this project's configuration and push it. The deployed workflow is a generated artifact like any other: without this it stays frozen at whatever the template said when 'assembly init' ran. Pushes only when the content actually differs.",
		c.cmdAssemblySyncWorkflow,
		strictcli.WithEffect(strictcli.EffectMutating),
		// Deliberately NOT consequential: it rewrites one tool-owned
		// generated file to match the generator, and rerunning converges.
		strictcli.WithGrants(assemblyCommitGrant),
		strictcli.WithFlags(
			strictcli.StringFlag("pin-selfdoc", "selfdoc version the regenerated workflow pins its 'go install' to. Omitted, the running binary's own version is used. The release path states it instead, because the binary a post-release hook finds was built before the version bump and its own version names a release the module proxy cannot serve yet.", strictcli.Optional()),
			strictcli.StringFlag("pin-pagefind", "pagefind version the regenerated workflow pins its toolchain install to. Omitted, PyPI's current release is used: pagefind is a CI-only tool nothing here installs, so there is no local version to read.", strictcli.Optional()),
		),
	)
}

// assemblyRepo reads the assembly repository the project declares, refusing
// when it declares none.
func (c *cli) assemblyRepo(cfg map[string]any) (string, strictcli.Outcome, bool) {
	repo := configString(cfg, "assembly", "repo")
	if repo == "" {
		return "", c.failf("Error: assembly.repo not configured in selfdoc.json."), false
	}
	return repo, strictcli.Exit(0), true
}

func (c *cli) cmdAssemblyInit(ctx *strictcli.Context, kwargs map[string]any) strictcli.Outcome {
	handle := effects.FromContext(ctx)

	cfg, outcome, ok := c.requireConfig()
	if !ok {
		return outcome
	}
	repo, outcome, ok := c.assemblyRepo(cfg)
	if !ok {
		return outcome
	}
	pagesProject := configString(cfg, "assembly", "pages_project")
	if pagesProject == "" {
		return c.failf("Error: assembly.pages_project not configured in selfdoc.json.")
	}
	canonicalBase := configString(cfg, "topology", "docs_base")
	if canonicalBase == "" {
		return c.failf("Error: topology.docs_base not configured in selfdoc.json.")
	}
	// The workflow init writes pins its toolchain exactly as the one
	// sync-workflow rewrites later, and refuses an unpublishable pin the same
	// way -- a fresh assembly must not start life with a deploy that cannot
	// install its own tools.
	pins, err := assembly.ResolveToolchainPins(assembly.PinOptions{
		SelfdocVersion: c.opts.Version,
		Registry:       c.opts.Registry,
	})
	if err != nil {
		return c.fail(err)
	}
	if err := assembly.CheckPinsArePublished(pins, c.opts.Registry); err != nil {
		return c.fail(err)
	}

	files, err := assembly.AssemblyInit(pagesProject, canonicalBase, pins)
	if err != nil {
		return c.fail(err)
	}

	c.printf("Creating repository %s...\n", repo)
	result, err := handle.Run(
		[]string{"gh", "repo", "create", repo, "--private"},
		effects.CaptureOutput(), effects.Timeout(30*time.Second),
		effects.Resource("gh-repo:"+repo), effects.Grant("create-repo"),
	)
	if err != nil {
		return c.fail(err)
	}
	if !result.Unsettled && result.ExitCode != 0 {
		return c.failf("Error: Failed to create repository: %s", strings.TrimSpace(result.StderrString()))
	}

	// Paths are pushed in sorted order: Go maps carry none of their own, and
	// sorted is the one order that is the same on every run.
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	for _, path := range paths {
		c.printf("  Creating %s...\n", path)
		encoded := base64.StdEncoding.EncodeToString([]byte(files[path]))
		payload, err := json.Marshal(map[string]any{
			"message": "Initial: " + path,
			"content": encoded,
		})
		if err != nil {
			return c.fail(err)
		}
		result, err := handle.Run(
			[]string{"gh", "api", "--method", "PUT",
				"/repos/" + repo + "/contents/" + path, "--input", "-"},
			effects.Stdin(payload), effects.CaptureOutput(),
			effects.Timeout(30*time.Second),
			effects.Resource("gh-contents:"+repo+"/"+path),
		)
		if err != nil {
			return c.fail(err)
		}
		if !result.Unsettled && result.ExitCode != 0 {
			return c.failf("Error: Failed to create %s: %s", path, strings.TrimSpace(result.StderrString()))
		}
	}

	account := firstEnv("CF_ACCOUNT_ID", "CLOUDFLARE_ACCOUNT_ID")
	token := firstEnv("CF_PAGES_API_TOKEN", "CLOUDFLARE_API_TOKEN")
	if account != "" && token != "" {
		env := environMap()
		env["CLOUDFLARE_ACCOUNT_ID"] = account
		env["CLOUDFLARE_API_TOKEN"] = token
		result, err := handle.Run(
			[]string{"npx", "wrangler", "pages", "project", "create", pagesProject, "--production-branch", "main"},
			effects.Env(env), effects.CaptureOutput(), effects.Timeout(60*time.Second),
			effects.Resource("cf-pages-project:"+pagesProject),
			effects.Grant("create-pages-project"),
		)
		if err != nil {
			return c.fail(err)
		}
		switch {
		case result.Unsettled:
		case result.ExitCode == 0:
			c.printf("Created CF Pages project: %s\n", pagesProject)
		default:
			c.eprintf("Warning: CF Pages project creation failed: %s\n", strings.TrimSpace(result.StderrString()))
		}
	} else {
		c.eprintf("Warning: CF_ACCOUNT_ID/CF_PAGES_API_TOKEN not set, skipping CF Pages project creation.\n")
	}

	for _, secret := range []struct{ name, value string }{
		{"CF_ACCOUNT_ID", account},
		{"CF_PAGES_API_TOKEN", token},
	} {
		if secret.value == "" {
			continue
		}
		result, err := handle.Run(
			[]string{"gh", "secret", "set", secret.name, "--repo", repo, "--body", secret.value},
			effects.CaptureOutput(), effects.Timeout(30*time.Second),
			effects.Resource("gh-secret:"+repo+"/"+secret.name),
			effects.Grant("set-secret"),
		)
		if err != nil {
			return c.fail(err)
		}
		switch {
		case result.Unsettled:
		case result.ExitCode == 0:
			c.printf("Set GitHub secret: %s\n", secret.name)
		default:
			c.eprintf("Warning: Failed to set %s secret: %s\n", secret.name,
				strings.TrimSpace(result.StderrString()))
		}
	}

	c.printf("Assembly repository initialized: %s\n", repo)
	return strictcli.Exit(0)
}

func (c *cli) cmdAssemblyPush(ctx *strictcli.Context, kwargs map[string]any) strictcli.Outcome {
	handle := effects.FromContext(ctx)

	cfg, outcome, ok := c.requireConfig()
	if !ok {
		return outcome
	}
	repo, outcome, ok := c.assemblyRepo(cfg)
	if !ok {
		return outcome
	}
	slug := configString(cfg, "topology", "slug")
	if slug == "" {
		return c.failf("Error: topology.slug not configured in selfdoc.json.")
	}

	result, err := handle.Run(
		[]string{"gh", "repo", "view", "--json", "nameWithOwner", "--jq", ".nameWithOwner"},
		effects.CaptureOutput(), effects.Timeout(15*time.Second), effects.Read(),
	)
	if err != nil {
		return c.fail(err)
	}
	if result.ExitCode != 0 {
		return c.failf("Error: Failed to detect source repository: %s",
			strings.TrimSpace(result.StderrString()))
	}
	sourceRepo := strings.TrimSpace(result.StdoutString())

	// A project that declares it has no public version has no tag to
	// dispatch at, so it is dispatched at the branch it is on and recorded
	// under the literal. Both halves are required: a version string would
	// name a release nobody made, and a ref is what the assembly clones.
	var version, ref string
	if config.IsUnversioned(cfg) {
		version = config.UnversionedVersion
		branch, outcome, ok := c.currentBranch(handle)
		if !ok {
			return outcome
		}
		if outcome, ok := c.requireBranchOnOrigin(handle, branch); !ok {
			return outcome
		}
		ref = branch
	} else {
		version, _ = cfg["version"].(string)
		if version == "" {
			version = util.DetectProjectVersion(c.dir(), "0.0.0")
		}

		// The assembly builds selfdoc.json's newest declared version, so a
		// 'versions' array that omits the version being dispatched would
		// publish something else under this version's name.
		if err := site.CheckVersionIsDeclared(cfg, version); err != nil {
			return c.fail(err)
		}

		// Resolve the tag that names THIS project's version. Never the
		// repository's newest tag: in a repo that releases more than one
		// thing, that is a sibling's tag and the assembly builds the wrong
		// source tree under this project's slug.
		tags, err := site.ListRepoTags(c.dir(), handle)
		if err != nil {
			return c.fail(err)
		}
		resolved, err := site.ResolveProjectTag(tags, version)
		if err != nil {
			return c.fail(err)
		}
		ref = resolved
	}

	dispatch := assembly.AssemblyPush(repo, sourceRepo, slug, version, ref)
	payload, err := dispatch.PayloadJSON()
	if err != nil {
		return c.fail(err)
	}
	result, err = handle.Run(
		[]string{"gh", "api", "--method", "POST", dispatch.Endpoint, "--input", "-"},
		effects.Stdin(payload), effects.CaptureOutput(), effects.Timeout(30*time.Second),
		effects.Resource("dispatch:"+repo), effects.Grant("assembly-dispatch"),
	)
	if err != nil {
		return c.fail(err)
	}
	if !result.Unsettled && result.ExitCode != 0 {
		return c.failf("Error: Failed to dispatch rebuild: %s", strings.TrimSpace(result.StderrString()))
	}

	c.printf("Dispatched assembly rebuild for %s %s (ref: %s)\n",
		slug, site.VersionLabel(version), ref)
	return strictcli.Exit(0)
}

func (c *cli) cmdAssemblyStatus(ctx *strictcli.Context, kwargs map[string]any) strictcli.Outcome {
	handle := effects.FromContext(ctx)

	cfg, outcome, ok := c.requireConfig()
	if !ok {
		return outcome
	}
	repo, outcome, ok := c.assemblyRepo(cfg)
	if !ok {
		return outcome
	}

	foundRuns := false
	for _, argv := range assembly.AssemblyStatus(repo) {
		result, err := handle.Run(argv,
			effects.CaptureOutput(), effects.Timeout(30*time.Second), effects.Read())
		if err != nil {
			return c.fail(err)
		}
		if result.ExitCode == 0 && strings.TrimSpace(result.StdoutString()) != "" {
			c.println(strings.TrimSpace(result.StdoutString()))
			foundRuns = true
		}
	}

	if !foundRuns {
		c.println("No recent assembly builds found.")
	}
	return strictcli.Exit(0)
}

func (c *cli) cmdAssemblyRebuild(ctx *strictcli.Context, kwargs map[string]any) strictcli.Outcome {
	handle := effects.FromContext(ctx)

	cfg, outcome, ok := c.requireConfig()
	if !ok {
		return outcome
	}
	repo, outcome, ok := c.assemblyRepo(cfg)
	if !ok {
		return outcome
	}

	// The membership record comes through the same reader every other remote
	// read uses, so an absent record and a failed read are told apart here
	// too: neither is an assembly with no projects, and both stop the
	// rebuild.
	raw, err := assembly.FetchRemoteText(handle, repo, site.ProjectsPath, false,
		"dispatch a rebuild for every project on "+repo)
	if err != nil {
		return c.fail(err)
	}

	var projects map[string]any
	if err := json.Unmarshal([]byte(raw), &projects); err != nil {
		// The only thing left to go wrong: the record was read successfully
		// and is not a JSON document. Every other failure -- the read itself,
		// the base64 decode -- is already a remote-read error above.
		return c.failf("Error: %s:%s is not valid JSON: %s", repo, site.ProjectsPath, err)
	}

	if len(projects) == 0 {
		c.println("No projects configured in assembly.")
		return strictcli.Exit(0)
	}

	dispatches, err := assembly.AssemblyRebuild(repo, projects)
	if err != nil {
		return c.fail(err)
	}

	for _, dispatch := range dispatches {
		c.printf("Dispatching rebuild for %s...\n", dispatch.Slug)
		payload, err := dispatch.PayloadJSON()
		if err != nil {
			return c.fail(err)
		}
		result, err := handle.Run(
			[]string{"gh", "api", "--method", "POST", dispatch.Endpoint, "--input", "-"},
			effects.Stdin(payload), effects.CaptureOutput(), effects.Timeout(30*time.Second),
			effects.Resource("dispatch:"+repo+"/"+dispatch.Slug),
			effects.Grant("assembly-dispatch"),
		)
		if err != nil {
			return c.fail(err)
		}
		switch {
		case result.Unsettled:
		case result.ExitCode != 0:
			c.eprintf("  Warning: Failed to dispatch for %s: %s\n", dispatch.Slug,
				strings.TrimSpace(result.StderrString()))
		default:
			c.printf("  Dispatched rebuild for %s.\n", dispatch.Slug)
		}
	}

	c.printf("Dispatched %d rebuild(s).\n", len(dispatches))
	return strictcli.Exit(0)
}

func (c *cli) cmdAssemblyRetire(ctx *strictcli.Context, kwargs map[string]any) strictcli.Outcome {
	slug := strictcli.Get[string](kwargs, "slug")
	handle := effects.FromContext(ctx)

	cfg, outcome, ok := c.requireConfig()
	if !ok {
		return outcome
	}
	repo, outcome, ok := c.assemblyRepo(cfg)
	if !ok {
		return outcome
	}

	summary, err := assembly.RetireProject(handle, repo, slug, assemblyDefaultBranch)
	if err != nil {
		return c.fail(err)
	}

	if outcome, ok := c.dispatchSharedRebuild(handle, repo); !ok {
		return outcome
	}

	remaining := strings.Join(summary.Remaining, ", ")
	if remaining == "" {
		remaining = "(none)"
	}
	c.printf("Retired %s from %s: %d path(s) deleted. Remaining projects: %s. "+
		"Shared elements will regenerate.\n", slug, repo, len(summary.Deleted), remaining)
	return strictcli.Exit(0)
}

func (c *cli) cmdAssemblyRedirects(ctx *strictcli.Context, kwargs map[string]any) strictcli.Outcome {
	slug := strictcli.Get[string](kwargs, "slug")
	docsBase := strictcli.Get[string](kwargs, "docs_base")
	fmt.Fprint(c.out(), site.GenerateRedirectsFile(slug, docsBase))
	return strictcli.Exit(0)
}

func (c *cli) cmdAssemblyGenerateShared(ctx *strictcli.Context, kwargs map[string]any) strictcli.Outcome {
	handle := effects.FromContext(ctx)

	// The generator's own API spells "not given" as the empty string; the CLI
	// spells absence as absence, so the two meet here.
	written, err := assembly.GenerateSharedFiles(assembly.SharedFilesOptions{
		SiteDir:       strictcli.Get[string](kwargs, "site_dir"),
		ManifestsDir:  strictcli.Get[string](kwargs, "manifests_dir"),
		CanonicalBase: strictcli.Get[string](kwargs, "canonical_base"),
		DocsBase:      optString(kwargs, "docs_base"),
		HomeSlug:      optString(kwargs, "home_slug"),
	}, handle)
	if err != nil {
		return c.fail(err)
	}

	c.printf("Generated %d shared file(s):\n", len(written))
	for _, path := range written {
		c.printf("  %s\n", path)
	}
	return strictcli.Exit(0)
}

func (c *cli) cmdAssemblyIntegrate(ctx *strictcli.Context, kwargs map[string]any) strictcli.Outcome {
	handle := effects.FromContext(ctx)

	summary, err := assembly.IntegrateProject(assembly.IntegrateOptions{
		Slug:          optString(kwargs, "slug"),
		Version:       optString(kwargs, "version"),
		Ref:           optString(kwargs, "ref"),
		SourceRepo:    optString(kwargs, "source_repo"),
		Scope:         optString(kwargs, "scope"),
		CanonicalBase: strictcli.Get[string](kwargs, "canonical_base"),
		AssemblyDir:   absentMeans(kwargs, "assembly_dir", "."),
		SourceDir:     optString(kwargs, "source_dir"),
		Branch:        absentMeans(kwargs, "branch", assemblyDefaultBranch),
		Attempts:      absentMeans(kwargs, "attempts", assembly.DefaultAttempts),
		RetryDelay:    assembly.DefaultRetryDelay,
		Stderr:        c.errOut(),
	}, handle)
	if err != nil {
		return c.fail(err)
	}

	name := summary.Slug
	if name == "" {
		name = "shared elements"
	}
	if summary.Committed {
		c.printf("Integrated %s scope for %s (attempt %d, %d shared file(s)).\n",
			summary.Scope, name, summary.Attempt, len(summary.Shared))
	} else {
		c.printf("Nothing to commit for %s (%s scope); the assembly is already current.\n",
			name, summary.Scope)
	}
	return strictcli.Exit(0)
}

func (c *cli) cmdAssemblyVerify(ctx *strictcli.Context, kwargs map[string]any) strictcli.Outcome {
	assemblyDir := strictcli.Get[string](kwargs, "assembly_dir")
	canonicalBase := strictcli.Get[string](kwargs, "canonical_base")

	report, err := verify.VerifyAssembly(assemblyDir, canonicalBase, nil, 0)
	if err != nil {
		return c.fail(err)
	}

	for _, skip := range report.Skipped {
		c.eprintf("NOT CHECKED: %s -- %s\n", skip.Check, skip.Reason)
	}

	if !report.OK() {
		c.eprintf("%s\n", report.ErrorText())
		return strictcli.Exit(1)
	}

	c.printf("The assembled tree at %s passed %d of %d check(s).\n",
		assemblyDir, len(report.Ran), len(verify.Checks))
	return strictcli.Exit(0)
}

func (c *cli) cmdAssemblyRepublishAll(ctx *strictcli.Context, kwargs map[string]any) strictcli.Outcome {
	handle := effects.FromContext(ctx)
	summary, err := assembly.RepublishAll(assembly.RepublishOptions{
		HomeDir:     strictcli.Get[string](kwargs, "home"),
		ProjectDirs: stringList(kwargs, "repo"),
		Running:     c.opts.Version,
	}, handle)
	if err != nil {
		return c.fail(err)
	}
	if !summary.Published {
		c.printf("Dry run: the checks passed and every project built. Would publish to %s (its deploy workflow pins selfdoc %s):\n", summary.Repo, summary.Pin)
	} else {
		c.printf("Republished every project to %s (its deploy workflow pins selfdoc %s):\n", summary.Repo, summary.Pin)
	}
	for _, project := range summary.Projects {
		role := ""
		if project.Home {
			role = ", the home project"
		}
		line := fmt.Sprintf("  %s %s%s: %d file(s) and its manifest", project.Slug, project.Version, role, len(project.Files))
		if project.Overlay != "" {
			line += "; converts " + project.Overlay + " to schema_version " + strconv.Itoa(manifest.SchemaVersion)
		}
		if project.Commit != "" {
			line += ", commit " + project.Commit
		}
		c.println(line)
	}
	if !summary.Published {
		c.println("then one shared-only deploy request. Nothing was published.")
	} else {
		c.println("Sent one shared-only deploy request; the shared elements will regenerate.")
	}
	return strictcli.Exit(0)
}

func (c *cli) cmdAssemblyPreview(ctx *strictcli.Context, kwargs map[string]any) strictcli.Outcome {
	handle := effects.FromContext(ctx)

	summary, err := preview.PreviewAssembly(
		strictcli.Get[string](kwargs, "home"),
		stringList(kwargs, "repo"),
		strictcli.Get[string](kwargs, "out"),
		strictcli.Get[string](kwargs, "canonical_base"),
		strictcli.Get[bool](kwargs, "build"),
		optString(kwargs, "theme"),
		handle,
	)
	if err != nil {
		return c.fail(err)
	}

	// The report first, and loudly. A preview serves a tree that failed
	// verification on purpose -- that is the state worth looking at -- so the
	// only thing standing between a broken tree and a wrong conclusion is
	// that the reader was told.
	c.eprintf("%s\n", preview.RenderReport(summary.Report, summary.OutDir))
	c.printf("Preview of %d project(s) (home: %s) at %s\n",
		len(summary.Slugs), summary.Home, summary.SiteDir)

	code, err := preview.ServePreview(summary.SiteDir, strictcli.Get[int](kwargs, "port"),
		func(port int) {
			c.printf("Preview: http://%s:%d/  (Ctrl-C to stop)\n", serving.Host, port)
		})
	if err != nil {
		return c.fail(err)
	}
	return strictcli.Exit(code)
}

func (c *cli) cmdAssemblySyncWorkflow(ctx *strictcli.Context, kwargs map[string]any) strictcli.Outcome {
	handle := effects.FromContext(ctx)

	cfg, outcome, ok := c.requireConfig()
	if !ok {
		return outcome
	}
	repo, outcome, ok := c.assemblyRepo(cfg)
	if !ok {
		return outcome
	}
	pagesProject := configString(cfg, "assembly", "pages_project")
	if pagesProject == "" {
		return c.failf("Error: assembly.pages_project not configured in selfdoc.json.")
	}
	canonicalBase := configString(cfg, "topology", "docs_base")
	if canonicalBase == "" {
		return c.failf("Error: topology.docs_base not configured in selfdoc.json.")
	}

	// Resolve every pin, then refuse any that the registry cannot serve --
	// before a single byte is written. A pin that resolves nowhere would
	// otherwise fail at the next dispatch, inside the assembly repo's CI.
	pins, err := assembly.ResolveToolchainPins(assembly.PinOptions{
		SelfdocVersion:  c.selfdocPin(optString(kwargs, "pin_selfdoc")),
		PagefindVersion: optString(kwargs, "pin_pagefind"),
		Registry:        c.opts.Registry,
	})
	if err != nil {
		return c.fail(err)
	}
	if err := assembly.CheckPinsArePublished(pins, c.opts.Registry); err != nil {
		return c.fail(err)
	}

	content, err := assembly.GenerateWorkflowYAML(pagesProject, canonicalBase, pins)
	if err != nil {
		return c.fail(err)
	}

	label := fmt.Sprintf("selfdoc %s, pagefind %s", pins.Selfdoc, pins.Pagefind)
	result, err := assembly.PushFilesToRepo(handle, repo,
		map[string][]byte{site.WorkflowPath: []byte(content)},
		"assembly: sync deploy workflow ("+label+")", assemblyDefaultBranch, nil)
	if err != nil {
		return c.fail(err)
	}

	if result.Changed {
		c.printf("Synced %s on %s (%s, commit %s).\n", site.WorkflowPath, repo, label, result.SHA)
	} else {
		c.printf("%s on %s is already current (%s); nothing pushed.\n",
			site.WorkflowPath, repo, label)
	}
	return strictcli.Exit(0)
}

// firstEnv returns the first of names that is set to a non-empty value.
func firstEnv(names ...string) string {
	for _, name := range names {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return ""
}

// environMap is this process's environment as a map, which is the shape the
// effects handle takes a complete child environment in.
func environMap() map[string]string {
	env := map[string]string{}
	for _, entry := range os.Environ() {
		if index := strings.IndexByte(entry, '='); index > 0 {
			env[entry[:index]] = entry[index+1:]
		}
	}
	return env
}

// currentBranch is the branch the project's checkout is on.
//
// A detached HEAD has no branch name to dispatch at, which is a refusal rather
// than a fallback: the assembly clones the ref it is handed, and there is no
// branch to hand it.
func (c *cli) currentBranch(handle *effects.Handle) (string, strictcli.Outcome, bool) {
	result, err := handle.Run(
		[]string{"git", "symbolic-ref", "--short", "HEAD"},
		effects.CaptureOutput(), effects.Timeout(15*time.Second),
		effects.Cwd(c.dir()), effects.Read(),
	)
	if err != nil {
		return "", c.fail(err), false
	}
	branch := strings.TrimSpace(result.StdoutString())
	if result.ExitCode != 0 || branch == "" {
		return "", c.failf(
			"Error: this checkout is not on a branch, so there is no ref to "+
				"dispatch an unversioned project at. A project declaring "+
				"'unversioned': true is cloned by the assembly at the branch "+
				"it is pushed from; check one out. (%s)",
			strings.TrimSpace(result.StderrString()),
		), false
	}
	return branch, strictcli.Exit(0), true
}

// requireBranchOnOrigin refuses a branch the assembly could not clone.
//
// The dispatch names a ref the deploy fetches from the source repository, so a
// branch that exists only in this checkout would send the deploy after
// something that is not there -- and the failure would surface minutes later,
// in a workflow log, rather than here.
func (c *cli) requireBranchOnOrigin(
	handle *effects.Handle,
	branch string,
) (strictcli.Outcome, bool) {
	result, err := handle.Run(
		[]string{"git", "ls-remote", "--exit-code", "origin", "refs/heads/" + branch},
		effects.CaptureOutput(), effects.Timeout(30*time.Second),
		effects.Cwd(c.dir()), effects.Read(),
	)
	if err != nil {
		return c.fail(err), false
	}
	if result.ExitCode != 0 {
		return c.failf(
			"Error: origin carries no branch %s, so the assembly cannot "+
				"clone this project at it. The dispatch names the ref the "+
				"deploy fetches; push the branch first. (%s)",
			util.PythonRepr(branch),
			strings.TrimSpace(result.StderrString()),
		), false
	}
	return strictcli.Exit(0), true
}
