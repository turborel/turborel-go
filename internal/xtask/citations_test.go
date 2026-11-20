package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// rulesFixture is a RULES.md that lists one decision and one rule.
const rulesFixture = "| D1 | Architecture |\n| R-TRACE-1 | Every test cites |\n"

// TestCheckTestTags takes each tag R-TRACE-1 allows, and refuses a test
// whose tag is missing or names nothing.
//
// Verifies: [TOOL] R-TRACE-1 a test's Verifies paragraph begins with a
// heading of the spec or the client's documents, a decision or a rule
// RULES.md lists, and a test without one fails verify.
func TestCheckTestTags(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, doc, problem string
	}{
		{"protocol", "// Verifies: [PROTOCOL Frames] a header.", ""},
		{"client", "// Verifies: [CLIENT Pipelines] a window.", ""},
		{"decision", "// Verifies: [DESIGN] D1 the architecture.", ""},
		{"rule", "// Verifies: [TOOL] R-TRACE-1 this check.", ""},
		{"second paragraph", "// TestX checks.\n//\n// Verifies: [DESIGN] D1 it.", ""},
		{"no paragraph", "// TestX checks.", "TestX has no Verifies paragraph (R-TRACE-1)"},
		{"no tag", "// Verifies: that it works.", "TestX's Verifies paragraph begins with no tag R-TRACE-1 takes"},
		{"decision unknown", "// Verifies: [DESIGN] D9 it.", "TestX cites D9, which RULES.md does not list"},
		{"rule unknown", "// Verifies: [TOOL] R-COV-9 it.", "TestX cites R-COV-9, which RULES.md does not list"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			decl := parseFunc(t, c.doc+"\nfunc TestX(t *testing.T) {}")
			if got := newCiter(rulesFixture).checkTest(decl); got != c.problem {
				t.Errorf("checkTest = %q, want %q", got, c.problem)
			}
		})
	}
}

// TestCheckTestSkipsOthers leaves alone functions `go test` does not run.
//
// Verifies: [TOOL] R-TRACE-1 only tests, fuzz tests, benchmarks and
// examples need a Verifies paragraph.
func TestCheckTestSkipsOthers(t *testing.T) {
	t.Parallel()

	known := newCiter(rulesFixture)
	for _, source := range []string{"func helper() {}", "func (s S) TestM() {}"} {
		if got := known.checkTest(parseFunc(t, source)); got != "" {
			t.Errorf("checkTest(%q) = %q, want nothing", source, got)
		}
	}

	if got := known.checkTest(&ast.GenDecl{Tok: token.VAR}); got != "" {
		t.Errorf("checkTest of a var = %q, want nothing", got)
	}

	for _, name := range []string{"TestA", "FuzzA", "BenchmarkA", "ExampleA"} {
		if !isTestName(name) {
			t.Errorf("isTestName(%q) = false, want true", name)
		}
	}
}

// TestCheckCommentHeadings refuses a heading the spec does not have, and
// a comment citing the file of open work.
//
// Verifies: [TOOL] R-TRACE-2 a citation of the spec or the client's
// documents names a heading they have, wrapped across lines or not.
func TestCheckCommentHeadings(t *testing.T) {
	t.Parallel()

	known := newCiter(rulesFixture)
	if got := known.checkComment("as the spec says [PROTOCOL A worked\nexample] and [CLIENT Pools]."); len(got) != 0 {
		t.Errorf("checkComment of known headings = %q, want nothing", got)
	}

	got := known.checkComment("[PROTOCOL Framing] [CLIENT Pool]")
	if len(got) != 2 || !strings.Contains(got[0], "[PROTOCOL Framing]") || !strings.Contains(got[1], "[CLIENT Pool]") {
		t.Errorf("checkComment of unknown headings = %q, want both refused", got)
	}
}

// TestCheckCommentOpenWork refuses a comment that cites the file of open
// work.
//
// Verifies: [TOOL] R-DOC-5 no comment cites the file of open work.
func TestCheckCommentOpenWork(t *testing.T) {
	t.Parallel()

	got := newCiter(rulesFixture).checkComment("see " + openWork)
	if len(got) != 1 || !strings.Contains(got[0], "R-DOC-5") {
		t.Errorf("checkComment = %q, want one R-DOC-5 problem", got)
	}
}

// parseFunc parses a file of the one declaration source gives.
func parseFunc(t *testing.T, source string) ast.Decl {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), "x_test.go", "package x\n"+source, parser.ParseComments)
	if err != nil {
		t.Fatalf("parsing %q: %v", source, err)
	}

	return file.Decls[0]
}
