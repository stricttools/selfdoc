package assembly

import "errors"

// Screen holds a public output of the assembly to the confidential-name rules
// of the lifecycle-and-license record, which the command wires in: Allow
// refuses a source checkout whose record lets no releasable publish public
// documentation, and Scan refuses a built tree whose pages name a term the
// machine-local confidential-name index protects, naming what (the output) in
// the refusal. Both are required: a publish without its screen is refused,
// never run unscreened.
type Screen struct {
	Allow func(sourceDir string) error
	Scan  func(what, dir string) error
}

// require refuses a screen missing either half.
func (s Screen) require(what string) error {
	if s.Allow == nil || s.Scan == nil {
		return errors.New(what + " needs its confidential-name screen (both its record check and its page scan); refusing to publish unscreened")
	}
	return nil
}
