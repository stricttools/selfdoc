module github.com/stricttools/selfdoc

go 1.26.3

toolchain go1.26.6

require (
	filippo.io/age v1.3.2
	github.com/stricttools/go-toml-edit v0.5.1
	github.com/stricttools/strictcli/go v0.39.1
	github.com/stricttools/testisolation/go v0.3.0
)

// chroma highlights code blocks, tinymoon composes the themes, brotli
// compresses the build output, strictspec generates the document validators,
// gotreesitter parses Python source in-process, x/text normalizes heading
// slugs, and playwright-go drives the browser suite behind the e2e build tag.
require (
	github.com/alecthomas/chroma/v2 v2.27.0
	github.com/andybalholm/brotli v1.2.4
	github.com/odvcencio/gotreesitter v0.52.0
	github.com/playwright-community/playwright-go v0.6000.0
	github.com/smm-h/tinymoon v0.11.0
	github.com/stricttools/strictspec/go v0.7.0
	golang.org/x/text v0.42.0
)

require (
	filippo.io/hpke v0.4.0 // indirect
	github.com/deckarep/golang-set/v2 v2.8.0 // indirect
	github.com/dlclark/regexp2/v2 v2.2.1 // indirect
	github.com/go-jose/go-jose/v3 v3.0.5 // indirect
	github.com/go-stack/stack v1.8.1 // indirect
	golang.org/x/crypto v0.55.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
)
