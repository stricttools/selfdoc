package cli

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/stricttools/selfdoc/internal/deploy"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/util"
	"github.com/smm-h/strictcli/go/strictcli"
)

func (c *cli) registerDeploy() {
	c.app.Command("deploy", "Deploy the built documentation site to the configured provider",
		c.cmdDeploy,
		strictcli.WithEffect(strictcli.EffectMutating),
		// Consequential: this is the one command whose effects leave the
		// machine and land on a live, publicly-visible site. The Cloudflare
		// Pages provider makes the uploaded tree live the moment it lands;
		// the GitHub Pages provider force-pushes gh-pages, so the previously
		// published tree is gone from the remote and is not recoverable
		// there. Neither can be undone by rerunning the command.
		strictcli.WithConsequential(),
		strictcli.WithGrants(
			strictcli.Grant{
				Name: "deploy",
				Reason: "publishes the built site to the configured Cloudflare Pages " +
					"project; the deployment is live the moment it lands",
				Kind: strictcli.ProcMutate,
			},
			strictcli.Grant{
				Name: "force-push",
				Reason: "replaces the remote gh-pages branch wholesale; the previous " +
					"published tree is not recoverable from the remote afterwards",
				Kind: strictcli.ProcMutate,
			},
		),
	)
}

func (c *cli) cmdDeploy(ctx *strictcli.Context, kwargs map[string]any) strictcli.Outcome {
	handle := effects.FromContext(ctx)

	cfg, outcome, ok := c.requireConfig()
	if !ok {
		return outcome
	}

	outputRel := strings.TrimRight(outputDirOf(cfg), "/")
	outputDir := filepath.Join(c.dir(), outputRel)
	if info, err := os.Stat(outputDir); err != nil || !info.IsDir() {
		return c.failf("Error: Output directory '%s' not found. Run 'selfdoc build' first.", outputRel)
	}

	deployConfig := configTable(cfg, "deploy")
	if len(deployConfig) == 0 {
		return c.failf("Error: No 'deploy' section in selfdoc.json. " +
			"Add a deploy provider configuration.")
	}

	// Prefer the version selfdoc.json declares, fall back to the project
	// manifest.
	version, _ := cfg["version"].(string)
	if version == "" {
		version = util.DetectProjectVersion(c.dir(), "0.0.0")
	}

	// The provider and its project are both settled by the config loader --
	// the provider against a closed set, the project as required for the one
	// provider that reads it -- so an unusable deploy block never reaches
	// here: it is refused where the config is read.
	provider, _ := deployConfig["provider"].(string)
	project, _ := deployConfig["project"].(string)

	// The project root is the deploy target: this command already operates on
	// one directory (config, output dir), so the repository whose origin
	// receives the force-push is stated, not inferred.
	if err := deploy.Dispatch(deploy.Config{Provider: provider, Project: project},
		outputDir, version, c.dir(), handle); err != nil {
		return c.failf("Deploy error: %s", err)
	}
	return strictcli.Exit(0)
}
