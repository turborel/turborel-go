package main

// The manual's layout and every document's links (RULES.md R-DOC-7, D4):
// each page of doc/ opens with its title and, but a volume's contents
// page, a line holding only the link to it; each volume's contents page
// lists its pages; and every relative link of a document resolves, those
// of doc/ within doc/, since the docs site reads doc/ alone.

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// The manual's directory, and the names its pages are kept by.
const (
	manualDir     = "doc"
	contentsPage  = "README.md"
	contentsLine  = "[Contents](README.md)"
	titlePrefix   = "# "
	markdownExt   = ".md"
	codeFence     = "```"
	contentsAtRow = 2
)

var (
	// markdownLink is an inline link's target.
	markdownLink = regexp.MustCompile(`\]\(([^)\s]+)\)`)
	// absoluteTarget is a link out of the repository, or to a place in
	// the same page.
	absoluteTarget = regexp.MustCompile(`^([a-z][a-z0-9+.-]*:|#)`)
)

// errManual is a page or a link out of place.
var errManual = errors.New("the manual and its links")

// manual checks the documents of the repository.
func manual(out io.Writer) error {
	report(out, "manual")

	problems, err := checkDocuments(".")
	if err != nil {
		return fmt.Errorf("manual: %w", err)
	}

	if len(problems) > 0 {
		return fmt.Errorf("%w:\n%s", errManual, strings.Join(problems, "\n"))
	}

	return nil
}

// checkDocuments gives each problem with the documents under root.
func checkDocuments(root string) ([]string, error) {
	pages, err := markdownFiles(root)
	if err != nil {
		return nil, err
	}

	var problems []string

	for _, page := range pages {
		text, err := os.ReadFile(page) //nolint:gosec // G304: a document of the repository, listed by walking it.
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", page, err)
		}

		problems = append(problems, checkLinks(root, page, string(text))...)
		problems = append(problems, checkPage(root, page, string(text))...)
	}

	return problems, nil
}

// markdownFiles lists the documents under root, leaving out hidden
// directories.
func markdownFiles(root string) ([]string, error) {
	var pages []string

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() && path != root && strings.HasPrefix(entry.Name(), ".") {
			return filepath.SkipDir
		}

		if !entry.IsDir() && strings.HasSuffix(entry.Name(), markdownExt) {
			pages = append(pages, path)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("listing documents: %w", err)
	}

	return pages, nil
}

// prose gives text without its fenced code, whose brackets are no links.
func prose(text string) string {
	var kept []string

	fenced := false

	for line := range strings.SplitSeq(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), codeFence) {
			fenced = !fenced

			continue
		}

		if !fenced {
			kept = append(kept, line)
		}
	}

	return strings.Join(kept, "\n")
}

// checkLinks gives each relative link of page that resolves to nothing,
// or, of a page of the manual, out of it.
func checkLinks(root, page, text string) []string {
	var problems []string

	inManual := isManual(root, page)

	for _, link := range markdownLink.FindAllStringSubmatch(prose(text), -1) {
		target := link[1]
		if absoluteTarget.MatchString(target) {
			continue
		}

		file, _, _ := strings.Cut(target, "#")
		path := filepath.Join(filepath.Dir(page), filepath.FromSlash(file))
		if _, err := os.Stat(path); err != nil {
			problems = append(problems, page+": the link "+target+" resolves to nothing")

			continue
		}

		if inManual && !isManual(root, path) {
			problems = append(problems, page+": the link "+target+" leaves doc/ and must be a full URL")
		}
	}

	return problems
}

// isManual says whether path is within root's doc/.
func isManual(root, path string) bool {
	rel, err := filepath.Rel(filepath.Join(root, manualDir), path)

	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// checkPage gives each problem with a page of the manual's layout.
func checkPage(root, page, text string) []string {
	if !isManual(root, page) {
		return nil
	}

	lines := strings.Split(text, "\n")

	var problems []string

	if !strings.HasPrefix(lines[0], titlePrefix) {
		problems = append(problems, page+": a page opens with its # Title")
	}

	if filepath.Base(page) == contentsPage {
		return append(problems, checkContents(page, text)...)
	}

	if len(lines) <= contentsAtRow || lines[contentsAtRow] != contentsLine {
		problems = append(problems, page+": a page's third line holds only "+contentsLine)
	}

	return problems
}

// checkContents gives each page of a volume its contents page does not
// list.
func checkContents(page, text string) []string {
	dir := filepath.Dir(page)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return []string{page + ": " + err.Error()}
	}

	var problems []string

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || name == contentsPage || !strings.HasSuffix(name, markdownExt) {
			continue
		}

		if !strings.Contains(text, "]("+name+")") && !strings.Contains(text, "]("+name+"#") {
			problems = append(problems, page+": the contents do not list "+name)
		}
	}

	return problems
}
