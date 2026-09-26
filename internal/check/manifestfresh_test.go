package check

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// writeManifest writes a manifest document recording the given pages and
// posts.
func writeManifest(t *testing.T, path string, pages, posts []any) {
	t.Helper()
	if posts == nil {
		posts = []any{}
	}
	document := map[string]any{
		"schema_version": 1,
		"name":           "test",
		"slug":           "test",
		"version":        "1.0.0",
		"description":    "",
		"language":       "python",
		"base_url":       "",
		"pages":          pages,
		"posts":          posts,
		"last_gen":       "",
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("encode manifest: %v", err)
	}
	write(t, path, string(encoded))
}

// manifestPage is one page entry of a manifest document.
func manifestPage(path, title string) any {
	return map[string]any{"path": path, "title": title, "type": "doc"}
}

// manifestPost is one post entry of a manifest document.
func manifestPost(path, title string) any {
	return map[string]any{
		"path": path, "title": title, "date": "2025-01-01",
		"slug": "hello", "tags": []any{},
	}
}

func TestManifestFreshness(t *testing.T) {
	for _, testCase := range []struct {
		name string
		// diskPages are the docs-tree files to write.
		diskPages []string
		// diskPosts are the post files to write.
		diskPosts []string
		// manifestPages and manifestPosts are what the manifest records.
		manifestPages []any
		manifestPosts []any
		// writeManifestFile decides whether a manifest exists at all.
		writeManifestFile bool
		// wantFiles are the paths the diagnostics must name, in order.
		wantFiles []string
		// wantMessages are fragments each diagnostic must carry, in
		// order.
		wantMessages []string
	}{
		{
			name:              "no manifest at all",
			diskPages:         []string{"index.md"},
			writeManifestFile: false,
		},
		{
			name:              "everything in step",
			diskPages:         []string{"index.md", "guide.md"},
			diskPosts:         []string{"hello.md"},
			manifestPages:     []any{manifestPage("index.md", "Index"), manifestPage("guide.md", "Guide")},
			manifestPosts:     []any{manifestPost("hello.md", "Hello")},
			writeManifestFile: true,
		},
		{
			name:              "a page on disk the manifest does not record",
			diskPages:         []string{"index.md", "new-page.md"},
			manifestPages:     []any{manifestPage("index.md", "Index")},
			writeManifestFile: true,
			wantFiles:         []string{"new-page.md"},
			wantMessages:      []string{"exists on disk but not in manifest"},
		},
		{
			name:              "a manifest page that is not on disk",
			diskPages:         []string{"index.md"},
			manifestPages:     []any{manifestPage("index.md", "Index"), manifestPage("gone.md", "Gone")},
			writeManifestFile: true,
			wantFiles:         []string{"gone.md"},
			wantMessages:      []string{"file not found on disk"},
		},
		{
			name:              "a post on disk the manifest does not record",
			diskPages:         []string{"index.md"},
			diskPosts:         []string{"hello.md"},
			manifestPages:     []any{manifestPage("index.md", "Index")},
			writeManifestFile: true,
			wantFiles:         []string{"hello.md"},
			wantMessages:      []string{"post exists on disk but not in manifest"},
		},
		{
			name:              "a manifest post that is not on disk",
			diskPages:         []string{"index.md"},
			manifestPages:     []any{manifestPage("index.md", "Index")},
			manifestPosts:     []any{manifestPost("hello.md", "Hello")},
			writeManifestFile: true,
			wantFiles:         []string{"hello.md"},
			wantMessages:      []string{"manifest lists post 'hello.md'"},
		},
		{
			name:              "an underscore-prefixed template is not a page",
			diskPages:         []string{"index.md", "_partial.md"},
			manifestPages:     []any{manifestPage("index.md", "Index")},
			writeManifestFile: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			isolate(t)
			root := t.TempDir()
			for _, page := range testCase.diskPages {
				write(t, filepath.Join(root, "stricttools", "docs", page), "# Page\n")
			}
			for _, post := range testCase.diskPosts {
				write(t, filepath.Join(root, "stricttools", "posts", post), "# Post\n")
			}
			if testCase.writeManifestFile {
				writeManifest(t,
					filepath.Join(root, "stricttools", ".docs-state", "manifest.json"),
					testCase.manifestPages, testCase.manifestPosts,
				)
			}
			projectConfig := map[string]any{
				"docs":  "stricttools/docs/",
				"posts": map[string]any{"dir": "stricttools/posts/"},
			}

			results, err := checkManifestFreshness(projectConfig, root)
			if err != nil {
				t.Fatalf("checkManifestFreshness: %v", err)
			}
			if len(results) != len(testCase.wantFiles) {
				t.Fatalf("diagnostics = %v, want %v",
					messagesOf(results), testCase.wantFiles)
			}
			for index, wantFile := range testCase.wantFiles {
				if results[index].File() != wantFile {
					t.Errorf("diagnostic %d file = %q, want %q",
						index, results[index].File(), wantFile)
				}
				if results[index].Code() != "STALE002" {
					t.Errorf("diagnostic %d code = %q, want STALE002",
						index, results[index].Code())
				}
			}
			for index, fragment := range testCase.wantMessages {
				if !strings.Contains(results[index].Message(), fragment) {
					t.Errorf("diagnostic %d message = %q, want it to carry %q",
						index, results[index].Message(), fragment)
				}
			}
		})
	}
}
