package main

// Traceability (RULES.md R-TRACE-1, R-TRACE-2, R-DOC-5): every test says
// what it verifies by a tag that names something real, every citation of
// the spec names one of its headings, and no comment cites the file of
// open work.

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// rulesFile is the file whose tables list the decisions and rules a
// test may cite.
const rulesFile = "RULES.md"

// verifiesPrefix begins the paragraph of a test's doc comment that says
// what it verifies.
const verifiesPrefix = "Verifies: "

// openWork is the file of open work, which no comment cites (R-DOC-5).
const openWork = "TODO.md"

// The prefixes of the functions `go test` runs, and the one that runs
// them.
const (
	testMain        = "TestMain"
	prefixTest      = "Test"
	prefixFuzz      = "Fuzz"
	prefixBenchmark = "Benchmark"
	prefixExample   = "Example"
)

// The kinds of citation of a document, as `[PROTOCOL Frames]`.
const (
	kindProtocol = "PROTOCOL"
	kindClient   = "CLIENT"
)

var (
	// documentCitation is a citation of a document's heading.
	documentCitation = regexp.MustCompile(`\[(PROTOCOL|CLIENT) ([^\]]+)\]`)
	// verifiesTag is the tag that must begin a Verifies paragraph: a
	// heading's citation, a decision or a tool's rule.
	verifiesTag = regexp.MustCompile(
		`^Verifies: (?:\[(?:PROTOCOL|CLIENT) [^\]]+\]|\[DESIGN\] (D\d+)|\[TOOL\] (R-[A-Z]+-\d+))`)
	// rulesRow is a row of RULES.md's tables of decisions and rules.
	rulesRow = regexp.MustCompile(`(?m)^\| (D\d+|R-[A-Z]+-\d+) \|`)
)

// errCitations is a citation missing or naming nothing.
var errCitations = errors.New("citations")

// citer knows what may be cited.
type citer struct {
	headings map[string]map[string]bool
	ids      map[string]bool
}

// citations checks every Go file of the repository.
func citations(out io.Writer) error {
	report(out, "citations")

	rules, err := os.ReadFile(rulesFile)
	if err != nil {
		return fmt.Errorf("citations: %w", err)
	}

	paths, err := goFiles(".")
	if err != nil {
		return fmt.Errorf("citations: %w", err)
	}

	known := newCiter(string(rules))

	var problems []string

	for _, path := range paths {
		found, err := known.checkFile(path)
		if err != nil {
			return err
		}

		problems = append(problems, found...)
	}

	if len(problems) > 0 {
		return fmt.Errorf("%w:\n%s", errCitations, strings.Join(problems, "\n"))
	}

	return nil
}

// newCiter knows the spec's headings, and the decisions and rules that
// rules, RULES.md's text, lists.
func newCiter(rules string) citer {
	known := citer{
		headings: map[string]map[string]bool{
			kindProtocol: lines(protocolHeadings),
			kindClient:   lines(clientHeadings),
		},
		ids: map[string]bool{},
	}
	for _, row := range rulesRow.FindAllStringSubmatch(rules, -1) {
		known.ids[row[1]] = true
	}

	return known
}

// lines gives the set of text's lines.
func lines(text string) map[string]bool {
	set := map[string]bool{}
	for line := range strings.SplitSeq(text, "\n") {
		set[line] = true
	}

	return set
}

// goFiles lists the Go files under root, leaving out hidden directories
// and testdata, as the go command does.
func goFiles(root string) ([]string, error) {
	var paths []string

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		name := entry.Name()
		if entry.IsDir() && path != root && (strings.HasPrefix(name, ".") || name == "testdata") {
			return filepath.SkipDir
		}

		if !entry.IsDir() && strings.HasSuffix(name, ".go") {
			paths = append(paths, path)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("listing Go files: %w", err)
	}

	return paths, nil
}

// checkFile gives each problem with the citations of the Go file at
// path.
func (c citer) checkFile(path string) ([]string, error) {
	files := token.NewFileSet()

	file, err := parser.ParseFile(files, path, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("citations: %w", err)
	}

	var problems []string

	for _, group := range file.Comments {
		for _, problem := range c.checkComment(group.Text()) {
			problems = append(problems, files.Position(group.Pos()).String()+": "+problem)
		}
	}

	if strings.HasSuffix(path, "_test.go") {
		for _, decl := range file.Decls {
			if problem := c.checkTest(decl); problem != "" {
				problems = append(problems, files.Position(decl.Pos()).String()+": "+problem)
			}
		}
	}

	return problems, nil
}

// checkComment gives each problem with one comment's citations.
func (c citer) checkComment(text string) []string {
	text = strings.Join(strings.Fields(text), " ")

	var problems []string

	for _, cited := range documentCitation.FindAllStringSubmatch(text, -1) {
		if !c.headings[cited[1]][cited[2]] {
			problems = append(problems, cited[0]+" cites a heading its document does not have")
		}
	}

	if strings.Contains(text, openWork) {
		problems = append(problems, "a comment cites "+openWork+" (R-DOC-5)")
	}

	return problems
}

// checkTest gives the problem with decl's citation if decl is a function
// `go test` runs, or "" if it has none.
func (c citer) checkTest(decl ast.Decl) string {
	function, ok := decl.(*ast.FuncDecl)
	if !ok || function.Recv != nil || !isTestName(function.Name.Name) {
		return ""
	}

	for paragraph := range strings.SplitSeq(function.Doc.Text(), "\n\n") {
		paragraph = strings.Join(strings.Fields(paragraph), " ")
		if !strings.HasPrefix(paragraph, verifiesPrefix) {
			continue
		}

		tag := verifiesTag.FindStringSubmatch(paragraph)
		if tag == nil {
			return function.Name.Name + "'s Verifies paragraph begins with no tag R-TRACE-1 takes"
		}

		for _, id := range tag[1:] {
			if id != "" && !c.ids[id] {
				return function.Name.Name + " cites " + id + ", which " + rulesFile + " does not list"
			}
		}

		return ""
	}

	return function.Name.Name + " has no Verifies paragraph (R-TRACE-1)"
}

// isTestName says whether a function of this name is one `go test` runs
// as a test: TestMain, which runs them, is not.
func isTestName(name string) bool {
	if name == testMain {
		return false
	}

	for _, prefix := range []string{prefixTest, prefixFuzz, prefixBenchmark, prefixExample} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}

	return false
}
