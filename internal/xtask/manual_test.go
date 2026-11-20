package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTree writes files, each a path relative to a temporary root and
// its text, and gives the root.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()

	root := t.TempDir()
	for name, text := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	return root
}

// TestManualLayoutKept passes a manual laid out as Turborel's.
//
// Verifies: [TOOL] R-DOC-7 a page opening with its title and its contents
// line, listed by its volume's contents page, whose links resolve within
// doc/, and links out of doc/ as full URLs, pass.
func TestManualLayoutKept(t *testing.T) {
	t.Parallel()

	root := writeTree(t, map[string]string{
		"README.md":          "# R\n\n[doc](doc/README.md) [x](https://example.org/y)\n",
		"doc/README.md":      "# Set\n\n[I](user/README.md)\n",
		"doc/user/README.md": "# Volume I\n\n[The set](../README.md)\n\n- [One](01-one.md#part)\n",
		"doc/user/01-one.md": "# One\n\n[Contents](README.md)\n\nSee [out](https://github.com/x/y).\n" +
			"\n```\n[no](link.md)\n```\n",
	})

	problems, err := checkDocuments(root)
	if err != nil {
		t.Fatal(err)
	}

	if len(problems) != 0 {
		t.Errorf("problems = %q, want none", problems)
	}
}

// TestManualLayoutRefused refuses each departure from the layout.
//
// Verifies: [TOOL] R-DOC-7 a page without its title or its contents line,
// a page its volume does not list, a link that resolves to nothing and a
// relative link out of doc/ each fail.
func TestManualLayoutRefused(t *testing.T) {
	t.Parallel()

	root := writeTree(t, map[string]string{
		"README.md":          "# R\n\n[gone](GONE.md)\n",
		"doc/README.md":      "# Set\n\n[I](user/README.md)\n",
		"doc/user/README.md": "# Volume I\n\n- [One](01-one.md)\n",
		"doc/user/01-one.md": "One\n\n[Contents](README.md)\n\n[up](../../README.md)\n",
		"doc/user/02-two.md": "# Two\n\nno contents line\n",
	})

	problems, err := checkDocuments(root)
	if err != nil {
		t.Fatal(err)
	}

	joined := strings.Join(problems, "\n")
	for _, want := range []string{
		"GONE.md resolves to nothing",
		"opens with its # Title",
		"leaves doc/ and must be a full URL",
		"third line holds only",
		"do not list 02-two.md",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("problems = %q, want one saying %q", problems, want)
		}
	}
}
