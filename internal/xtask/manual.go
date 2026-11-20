package main

// The manual's layout and every document's links (RULES.md R-DOC-7, D4):
// each page of doc/ opens with its title and, but a volume's contents
// page, a line holding only the link to it; each volume's contents page
// lists its pages; user/01-installing.md is there; doc/ holds no image;
// every relative link of a document resolves within the repository; and
// a page of doc/ links to this repository's own files relatively, never
// by their GitHub address, as the docs site points such links at GitHub
// at the same tag.

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
	// installingPage is the chapter the docs site keeps by its name.
	installingPage = "user/01-installing.md"
	// ownAddress begins the GitHub address of this repository's files,
	// which a page of doc/ links to relatively instead.
	ownAddress = "https://github.com/turborel/turborel-go/"
)

var (
	// markdownLink is an inline link's target.
	markdownLink = regexp.MustCompile(`\]\(([^)\s]+)\)`)
	// absoluteTarget is a link out of the repository, or to a place in
	// the same page.
	absoluteTarget = regexp.MustCompile(`^([a-z][a-z0-9+.-]*:|#)`)
	// image is a picture in a page, or a file of one.
	image = regexp.MustCompile(`(?i)!\[|\.(png|jpe?g|gif|svg|webp|bmp|ico)$`)
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

	problems := checkManualFiles(root)

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

// checkManualFiles gives each problem with doc/'s files: its installing
// chapter missing, or an image in it.
func checkManualFiles(root string) []string {
	var problems []string

	if _, err := os.Stat(filepath.Join(root, manualDir, filepath.FromSlash(installingPage))); err != nil {
		problems = append(problems, manualDir+"/"+installingPage+" is missing")
	}

	_ = filepath.WalkDir(filepath.Join(root, manualDir), func(path string, entry fs.DirEntry, err error) error {
		if err == nil && !entry.IsDir() && image.MatchString(entry.Name()) {
			problems = append(problems, path+": doc/ holds no images")
		}

		return nil
	}) // A walk that fails finds nothing, and the installing chapter's check says so.

	return problems
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

	if inManual && image.MatchString(prose(text)) {
		problems = append(problems, page+": doc/ holds no images")
	}

	for _, link := range markdownLink.FindAllStringSubmatch(prose(text), -1) {
		target := link[1]
		if inManual && strings.HasPrefix(target, ownAddress) {
			problems = append(problems, page+": the link "+target+" is to this repository, and is relative")

			continue
		}

		if absoluteTarget.MatchString(target) {
			continue
		}

		file, _, _ := strings.Cut(target, "#")
		path := filepath.Join(filepath.Dir(page), filepath.FromSlash(file))
		if _, err := os.Stat(path); err != nil || !within(root, path) {
			problems = append(problems, page+": the link "+target+" resolves to nothing in the repository")
		}
	}

	return problems
}

// isManual says whether path is within root's doc/.
func isManual(root, path string) bool {
	return within(filepath.Join(root, manualDir), path)
}

// within says whether path is dir or under it.
func within(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)

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
