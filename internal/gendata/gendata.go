// Package gendata generates data files by running sandboxed scripts via
// bubblewrap (bwrap).
//
// A project declares its scripts under the "gen_data" key of selfdoc.json;
// each declaration names the command to run, the file it must produce inside
// the generated-data directory, and the paths the sandbox mounts read-only. [GenerateData]
// runs each one inside bwrap with no network, no inherited environment and no
// writable path other than the output directory, then validates the produced
// file against its extension.
package gendata

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/util"
)

// Error is the failure every gen-data operation reports: a malformed script
// declaration, a missing bwrap, a script that failed, timed out or produced
// nothing, and an output file that does not parse as its extension claims.
//
// It is the Go counterpart of the Python surface's GenDataError, and the one
// error type a caller needs to recognize with errors.As to render "gen-data"
// diagnostics distinctly from an unexpected internal failure.
type Error struct {
	// Message is the diagnostic, rendered verbatim by Error.
	Message string
}

// Error returns the diagnostic.
func (e *Error) Error() string { return e.Message }

// scriptTimeout is how long a single sandboxed script may run. It is not an
// operand: the declaration says what to run, never how long it may take. It is
// a var only so the suite can shorten it instead of waiting a real minute.
var scriptTimeout = 60 * time.Second

// systemPaths are the read-only system directories the sandbox binds when they
// exist, in the order bwrap receives them.
var systemPaths = []string{"/usr", "/lib", "/lib64", "/bin", "/sbin"}

// pythonCSVFieldLimit is the default field size Python's csv module accepts
// before raising, reproduced so a CSV that Python would reject is rejected
// here with the same wording.
const pythonCSVFieldLimit = 131072

// GenerateData runs the sandboxed scripts declared in config and returns the
// paths of the files they produced, in declaration order.
//
// config is the decoded selfdoc.json object: the scripts are read from its
// "gen_data" object's "scripts" array, and a config declaring neither returns
// no paths and runs nothing. baseDir is the project directory every relative
// mount and the sandbox's working directory resolve against; the output
// directory is always baseDir joined with selfdoc's generated-data directory,
// created before the first script
// runs.
//
// Each returned path is that directory joined with the script's
// declared output name, so a relative baseDir yields relative paths.
//
// Under a handle in preview mode every script is recorded rather than run: the
// returned paths then name the files the scripts would have produced, and no
// output is validated, because nothing was produced.
func GenerateData(config map[string]any, baseDir string, h *effects.Handle) ([]string, error) {
	scripts, err := scriptsOf(config)
	if err != nil {
		return nil, err
	}
	if len(scripts) == 0 {
		return nil, nil
	}

	// Every declaration is checked before anything is created or run: a
	// malformed script is the config's defect, and it is reported whatever
	// the machine has installed and whatever the repository has granted.
	for _, script := range scripts {
		if err := validateScript(script); err != nil {
			return nil, err
		}
	}

	if err := checkBwrap(); err != nil {
		return nil, err
	}

	outputDir := util.PathJoin(baseDir, layout.DataRel)
	if err := layout.EnsureDir(h, baseDir, layout.DataRel); err != nil {
		return nil, err
	}

	var generated []string

	for _, script := range scripts {
		command, err := stringField(script, "command")
		if err != nil {
			return nil, err
		}
		output, err := stringField(script, "output")
		if err != nil {
			return nil, err
		}

		bwrapCmd, err := buildBwrapCommand(script, baseDir, outputDir)
		if err != nil {
			return nil, err
		}

		result, err := h.Run(
			bwrapCmd,
			effects.CaptureOutput(),
			effects.Timeout(scriptTimeout),
			effects.Resource("gendata:"+output),
		)
		if err != nil {
			if errors.Is(err, effects.ErrTimeout) {
				return nil, &Error{fmt.Sprintf("script timed out after 60 seconds: %s", command)}
			}
			return nil, err
		}

		outputPath := util.PathJoin(outputDir, output)

		if result.Unsettled {
			// Recorded, not run: nothing produced the file, so there is
			// nothing to test for existence and nothing to parse. The path
			// still names what the script would have produced.
			generated = append(generated, outputPath)
			continue
		}

		if result.ExitCode != 0 {
			return nil, &Error{fmt.Sprintf(
				"script failed with exit code %d: %s\nstderr: %s",
				result.ExitCode, command, result.Stderr,
			)}
		}

		if info, err := os.Stat(outputPath); err != nil || !info.Mode().IsRegular() {
			return nil, &Error{fmt.Sprintf(
				"script did not produce expected output file: %s", outputPath,
			)}
		}

		if err := validateOutput(outputPath); err != nil {
			return nil, err
		}
		generated = append(generated, outputPath)
	}

	return generated, nil
}

// scriptsOf reads config's gen_data.scripts array.
//
// A config with no gen_data section, no scripts key or an empty scripts array
// all mean the same thing: nothing to run. A section or an entry of the wrong
// shape is an error -- the Python this replaces raised an uncaught TypeError
// there, which is not a behavior worth reproducing.
func scriptsOf(config map[string]any) ([]map[string]any, error) {
	raw, ok := config["gen_data"]
	if !ok || raw == nil {
		return nil, nil
	}
	section, ok := raw.(map[string]any)
	if !ok {
		return nil, &Error{"'gen_data' must be an object"}
	}
	rawScripts, ok := section["scripts"]
	if !ok || rawScripts == nil {
		return nil, nil
	}
	items, ok := asList(rawScripts)
	if !ok {
		return nil, &Error{"'gen_data.scripts' must be a list of script declarations"}
	}
	scripts := make([]map[string]any, 0, len(items))
	for i, item := range items {
		script, ok := item.(map[string]any)
		if !ok {
			return nil, &Error{fmt.Sprintf("script declaration %d must be an object", i)}
		}
		scripts = append(scripts, script)
	}
	return scripts, nil
}

// validateScript reports whether a script declaration carries every required
// field, naming all the missing ones at once.
func validateScript(script map[string]any) error {
	var missing []string
	for _, field := range []string{"command", "output", "mounts"} {
		if _, ok := script[field]; !ok {
			missing = append(missing, field)
		}
	}
	if len(missing) > 0 {
		return &Error{fmt.Sprintf(
			"script declaration missing required field(s): %s", strings.Join(missing, ", "),
		)}
	}
	if _, ok := asList(script["mounts"]); !ok {
		return &Error{"'mounts' must be a list of paths"}
	}
	return nil
}

// checkBwrap reports whether bwrap is installed, naming the install command
// for both distribution families when it is not.
func checkBwrap() error {
	if _, err := exec.LookPath("bwrap"); err != nil {
		return &Error{
			"gen-data requires bubblewrap (bwrap). Install it: " +
				"sudo dnf install bubblewrap (Fedora) or " +
				"sudo apt install bubblewrap (Debian/Ubuntu)",
		}
	}
	return nil
}

// buildBwrapCommand builds the argv that runs one script declaration inside
// the sandbox.
//
// The order is the sandbox's contract and is reproduced as declared: the three
// isolation flags first, then one read-only bind per declared mount, the
// writable bind of the output directory, the system directories that exist, a
// fresh /proc and /dev, the working directory, and finally the command itself
// after the "--" separator, split on whitespace.
func buildBwrapCommand(script map[string]any, baseDir, outputDir string) ([]string, error) {
	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		return nil, err
	}
	absOutput, err := filepath.Abs(outputDir)
	if err != nil {
		return nil, err
	}

	cmd := []string{
		"bwrap",
		"--die-with-parent",
		"--unshare-all",
		"--clearenv",
	}

	// Read-only mounts for script dependencies.
	mounts, _ := asList(script["mounts"])
	for i, item := range mounts {
		mount, ok := item.(string)
		if !ok {
			return nil, &Error{fmt.Sprintf("mount %d must be a path string", i)}
		}
		// PathJoin, not filepath.Join: an absolute mount replaces the base
		// directory rather than being appended to it, as posixpath.join does.
		absMount, err := filepath.Abs(util.PathJoin(absBase, mount))
		if err != nil {
			return nil, err
		}
		cmd = append(cmd, "--ro-bind", absMount, absMount)
	}

	// Read-write bind for the output directory.
	cmd = append(cmd, "--bind", absOutput, absOutput)

	// System binaries needed to run scripts.
	for _, sysPath := range systemPaths {
		if _, err := os.Stat(sysPath); err == nil {
			cmd = append(cmd, "--ro-bind", sysPath, sysPath)
		}
	}

	// Basic filesystem.
	cmd = append(cmd, "--proc", "/proc", "--dev", "/dev")

	// Working directory.
	cmd = append(cmd, "--chdir", absBase)

	// The actual command.
	command, err := stringField(script, "command")
	if err != nil {
		return nil, err
	}
	cmd = append(cmd, "--")
	cmd = append(cmd, strings.Fields(command)...)

	return cmd, nil
}

// validateOutput reports whether the file at path parses as its extension
// claims: JSON is decoded, CSV is read to the end, and any other extension is
// left unchecked.
func validateOutput(path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return &Error{fmt.Sprintf("cannot read output file %s: %v", path, err)}
	}

	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		var parsed any
		if err := json.Unmarshal(content, &parsed); err != nil {
			return &Error{fmt.Sprintf("output file %s is not valid JSON: %v", path, err)}
		}
	case ".csv":
		if err := readCSV(string(content)); err != nil {
			return &Error{fmt.Sprintf("output file %s is not valid CSV: %v", path, err)}
		}
	}
	return nil
}

// readCSV reads content the way Python's csv.reader does, reporting the two
// failures that reader can raise.
//
// Go's encoding/csv is much stricter than Python's: it rejects a bare quote
// inside a quoted field and a row whose field count differs from the first
// row's, neither of which Python's reader objects to. Routing this check
// through it would reject files the Python accepted, so the scan below
// reproduces Python's parser instead -- a NUL byte anywhere and a field longer
// than the module's default limit are the only rejections, with Python's own
// wording.
func readCSV(content string) error {
	if strings.ContainsRune(content, 0) {
		return errors.New("line contains NUL")
	}
	field := 0
	quoted := false
	for i := 0; i < len(content); i++ {
		c := content[i]
		switch {
		case quoted:
			if c == '"' {
				if i+1 < len(content) && content[i+1] == '"' {
					i++
					field++
					continue
				}
				quoted = false
				continue
			}
			field++
		case c == '"':
			quoted = true
		case c == ',':
			field = 0
		case c == '\n' || c == '\r':
			field = 0
		default:
			field++
		}
		if field > pythonCSVFieldLimit {
			return fmt.Errorf("field larger than field limit (%d)", pythonCSVFieldLimit)
		}
	}
	return nil
}

// stringField reads a string-valued field out of a script declaration.
func stringField(script map[string]any, field string) (string, error) {
	value, ok := script[field].(string)
	if !ok {
		return "", &Error{fmt.Sprintf("'%s' must be a string", field)}
	}
	return value, nil
}

// asList accepts either shape a JSON array reaches this package in: the
// []any a decoder produces, and the []string a Go caller building the
// declaration by hand would write.
func asList(value any) ([]any, bool) {
	switch typed := value.(type) {
	case []any:
		return typed, true
	case []string:
		items := make([]any, 0, len(typed))
		for _, item := range typed {
			items = append(items, item)
		}
		return items, true
	default:
		return nil, false
	}
}
