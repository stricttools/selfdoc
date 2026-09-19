// Package fakegh is the fake gh the assembly suite puts at the front of PATH.
//
// It lives in its own package, outside the test binary, so the tests can build
// it ONCE into a plain uninstrumented executable: the assembly suite makes
// hundreds of gh calls, and a fake implemented by re-running the suite's own
// race-instrumented binary paid the race runtime's start-up cost on every one
// of them, which took the package's `go test -race` run to the edge of Go's
// per-binary timeout.
//
// The protocol is files in a state directory, which the test writes and the
// fake reads and updates. Two modes, chosen by what the directory holds. A
// "script.json" answers each call from a list in order, which is how a test
// asserts the exact sequence of API calls a push makes and how it injects a
// failure at one step. Otherwise the fake is a repository: it holds blobs,
// reports their real git blob hashes to the Trees API, applies a tree the way
// the API does -- an uploaded blob per entry, a null sha as a deletion -- and
// answers the Contents API from the same table. That is what makes an
// idempotence assertion mean anything: the second run reads back the bytes the
// first one wrote.
package fakegh

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/stricttools/selfdoc/internal/blog/site"
)

// StateEnv names the directory the fake keeps its state in. The test sets it
// in its own environment, and the fake inherits it through the subprocess the
// code under test starts.
const StateEnv = "SELFDOC_ASSEMBLY_FAKE_GH_STATE"

// CallRecord is one recorded invocation of the fake gh.
type CallRecord struct {
	// Argv is everything after the program name.
	Argv []string `json:"argv"`
	// Input is the request body the call wrote to standard input.
	Input string `json:"input"`
}

// Joined renders the argv the way an assertion reads it.
func (r CallRecord) Joined() string { return strings.Join(r.Argv, " ") }

// Response is one scripted answer.
type Response struct {
	// Code is the exit status.
	Code int `json:"code"`
	// Stdout and Stderr are the streams the call writes.
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
}

// Failure injects a failure into repo mode for every call whose argv contains
// Match.
type Failure struct {
	// Match is the substring of the joined argv this failure applies to.
	Match string `json:"match"`
	// Code is the exit status to answer with.
	Code int `json:"code"`
	// Stderr is what the call writes to standard error.
	Stderr string `json:"stderr"`
}

// Run answers one gh invocation against the state directory and returns its
// exit status.
func Run(dir string, argv []string) int {
	input := readAllStdinIfRequested(argv)
	recordCall(dir, argv, input)
	joined := strings.Join(argv, " ")

	if responses, ok := loadScript(dir); ok {
		index := bumpCounter(dir, "script")
		if index > len(responses) {
			fmt.Fprintf(os.Stderr,
				"fake gh: call %d has no scripted answer: %s\n", index, joined)
			return 99
		}
		answer := responses[index-1]
		os.Stdout.WriteString(answer.Stdout)
		os.Stderr.WriteString(answer.Stderr)
		return answer.Code
	}

	for _, failure := range loadFailures(dir) {
		if strings.Contains(joined, failure.Match) {
			os.Stderr.WriteString(failure.Stderr)
			return failure.Code
		}
	}
	return repoModeAnswer(dir, argv, joined, input)
}

// repoModeAnswer answers one call against the fake repository's own state.
func repoModeAnswer(dir string, argv []string, joined, input string) int {
	switch {
	case strings.Contains(joined, "/git/ref/heads/"):
		fmt.Print("headsha")
		return 0
	case strings.Contains(joined, "/git/commits/headsha"):
		fmt.Print("basetree")
		return 0
	case strings.Contains(joined, "/git/trees/basetree"):
		return answerTree(dir)
	case strings.Contains(joined, "/git/blobs"):
		return acceptBlob(dir, input)
	case strings.Contains(joined, "/git/trees"):
		return applyTree(dir, input)
	case strings.Contains(joined, "/git/commits"):
		bumpCounter(dir, "commits")
		fmt.Print("newcommit")
		return 0
	case strings.Contains(joined, "/git/refs/heads/"):
		fmt.Print("newcommit")
		return 0
	case strings.Contains(joined, "/contents/"):
		return answerContents(dir, argv)
	case strings.Contains(joined, "/actions/runs"):
		fmt.Print("{\"status\":\"completed\"}")
		return 0
	case strings.Contains(joined, "/dispatches"):
		return 0
	}
	fmt.Fprintf(os.Stderr, "fake gh: unrouted call: %s\n", joined)
	return 98
}

// answerTree reports every blob the branch holds, with its real git hash.
func answerTree(dir string) int {
	type entry struct {
		Path string `json:"path"`
		Type string `json:"type"`
		Mode string `json:"mode"`
		SHA  string `json:"sha"`
	}
	blobs := readBlobs(dir)
	paths := make([]string, 0, len(blobs))
	for path := range blobs {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	entries := make([]entry, 0, len(paths))
	for _, path := range paths {
		data, _ := base64.StdEncoding.DecodeString(blobs[path])
		entries = append(entries, entry{
			Path: path, Type: "blob", Mode: "100644",
			SHA: site.GitBlobSHA1(data),
		})
	}
	document := map[string]any{
		"truncated": readFlag(dir, "truncated.json"),
		"tree":      entries,
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fake gh: %v\n", err)
		return 97
	}
	os.Stdout.Write(encoded)
	return 0
}

// acceptBlob stores one uploaded blob under a fresh sha and answers with it.
func acceptBlob(dir, input string) int {
	var payload struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := json.Unmarshal([]byte(input), &payload); err != nil {
		fmt.Fprintf(os.Stderr, "fake gh: unreadable blob payload: %v\n", err)
		return 96
	}
	index := bumpCounter(dir, "uploads")
	sha := fmt.Sprintf("newblob%d", index)
	pending := readTable(dir, "pending.json")
	pending[sha] = payload.Content
	writeTable(dir, "pending.json", pending)
	fmt.Print(sha)
	return 0
}

// applyTree applies a tree request to the fake repository: an entry naming an
// uploaded blob writes it, and an entry with a null sha deletes the path.
func applyTree(dir, input string) int {
	var payload struct {
		BaseTree string `json:"base_tree"`
		Tree     []struct {
			Path string  `json:"path"`
			SHA  *string `json:"sha"`
		} `json:"tree"`
	}
	if err := json.Unmarshal([]byte(input), &payload); err != nil {
		fmt.Fprintf(os.Stderr, "fake gh: unreadable tree payload: %v\n", err)
		return 95
	}
	blobs := readBlobs(dir)
	pending := readTable(dir, "pending.json")
	for _, item := range payload.Tree {
		if item.SHA == nil {
			delete(blobs, item.Path)
			continue
		}
		content, ok := pending[*item.SHA]
		if !ok {
			fmt.Fprintf(os.Stderr,
				"fake gh: tree names blob %s, which was never uploaded\n", *item.SHA)
			return 94
		}
		blobs[item.Path] = content
	}
	writeTable(dir, "blobs.json", blobs)
	fmt.Print("newtree")
	return 0
}

// answerContents answers the Contents API from the same file table, with an
// absent path reported the way gh reports one.
func answerContents(dir string, argv []string) int {
	path := ""
	for _, arg := range argv {
		if index := strings.Index(arg, "/contents/"); index >= 0 {
			path = arg[index+len("/contents/"):]
		}
	}
	blobs := readBlobs(dir)
	encoded, ok := blobs[path]
	if !ok {
		fmt.Fprintf(os.Stderr, "gh: Not Found (HTTP 404)\n")
		return 1
	}
	fmt.Print(encoded)
	return 0
}

// readAllStdinIfRequested reads the request body, but only for a call that
// declared one: a call with no "--input -" has no body, and reading an
// inherited standard input would block.
func readAllStdinIfRequested(argv []string) string {
	wants := false
	for _, arg := range argv {
		if arg == "--input" {
			wants = true
		}
	}
	if !wants {
		return ""
	}
	var builder strings.Builder
	buffer := make([]byte, 4096)
	for {
		read, err := os.Stdin.Read(buffer)
		builder.Write(buffer[:read])
		if err != nil {
			break
		}
	}
	return builder.String()
}

// recordCall appends one invocation to the call log.
func recordCall(dir string, argv []string, input string) {
	encoded, err := json.Marshal(CallRecord{Argv: argv, Input: input})
	if err != nil {
		return
	}
	file, err := os.OpenFile(
		filepath.Join(dir, "calls.jsonl"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644,
	)
	if err != nil {
		return
	}
	defer file.Close()
	file.Write(append(encoded, '\n'))
}

// loadScript reads the scripted answers, reporting whether the fake is in
// scripted mode at all.
func loadScript(dir string) ([]Response, bool) {
	data, err := os.ReadFile(filepath.Join(dir, "script.json"))
	if err != nil {
		return nil, false
	}
	var responses []Response
	if err := json.Unmarshal(data, &responses); err != nil {
		return nil, false
	}
	return responses, true
}

// loadFailures reads the injected failures.
func loadFailures(dir string) []Failure {
	data, err := os.ReadFile(filepath.Join(dir, "failures.json"))
	if err != nil {
		return nil
	}
	var failures []Failure
	json.Unmarshal(data, &failures)
	return failures
}

// readBlobs is the fake repository's file table, path -> base64 content.
func readBlobs(dir string) map[string]string {
	return readTable(dir, "blobs.json")
}

// readTable reads one string-to-string state file.
func readTable(dir, name string) map[string]string {
	table := map[string]string{}
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return table
	}
	json.Unmarshal(data, &table)
	return table
}

// writeTable writes one string-to-string state file.
func writeTable(dir, name string, table map[string]string) {
	data, err := json.Marshal(table)
	if err != nil {
		return
	}
	os.WriteFile(filepath.Join(dir, name), data, 0o644)
}

// readFlag reads a boolean state file.
func readFlag(dir, name string) bool {
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(data)) == "true"
}

// bumpCounter increments a counter file and returns its new value.
func bumpCounter(dir, name string) int {
	path := filepath.Join(dir, name+".count")
	count := 0
	if data, err := os.ReadFile(path); err == nil {
		fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &count)
	}
	count++
	os.WriteFile(path, []byte(fmt.Sprintf("%d", count)), 0o644)
	return count
}
