package cli

import (
	"github.com/stricttools/strictcli/go/strictcli"
)

// newProbeApp builds a throwaway application whose one command emits value
// under schema, which is how a document is put through the framework's own
// payload validation without registering anything on the real tree.
func newProbeApp(schema map[string]any, value any) *strictcli.App {
	app := strictcli.NewApp("probe", "0.0.0", "schema probe")
	app.Command("emit", "emit the document",
		func(ctx *strictcli.Context, kwargs map[string]any) strictcli.Outcome {
			ctx.Payload(value)
			return strictcli.Exit(0)
		},
		strictcli.WithEffect(strictcli.EffectReadOnly),
		strictcli.PayloadSchema(schema),
	)
	return app
}
