package testset

import (
	"reflect"
	"testing"

	"github.com/bbsnly/sdlc/internal/config"
)

func matcher(dirs, globs []string) Matcher {
	return New(config.TestPaths{Dirs: dirs, FileGlobs: globs})
}

func TestADirectoryCoversEverythingBeneathIt(t *testing.T) {
	m := matcher([]string{"tests/", "spec"}, nil)
	for _, p := range []string{"tests/a.py", "tests/deep/nested/b.py", "spec/c.rb"} {
		if !m.Match(p) {
			t.Errorf("%s is under a test directory but did not match", p)
		}
	}
}

// A path that merely starts with the same letters is a different path, and a
// rule that got this wrong would freeze a directory nobody asked it to.
func TestADirectoryIsADirectoryNotAPrefix(t *testing.T) {
	m := matcher([]string{"test/"}, nil)
	for _, p := range []string{"testing/a.go", "test.go", "tester/b.go"} {
		if m.Match(p) {
			t.Errorf("%s matched the test directory rule", p)
		}
	}
}

// Every project writes "*_test.go", not "**/*_test.go", so a pattern has to
// reach the file's own name wherever it sits.
func TestAPatternMatchesTheFileNameAtAnyDepth(t *testing.T) {
	m := matcher(nil, []string{"*_test.go", "test_*.py"})
	for _, p := range []string{"x_test.go", "internal/store/x_test.go", "a/b/test_thing.py"} {
		if !m.Match(p) {
			t.Errorf("%s did not match", p)
		}
	}
	for _, p := range []string{"internal/store/store.go", "testdata/fixture.json"} {
		if m.Match(p) {
			t.Errorf("%s matched, but it is not a test", p)
		}
	}
}

// A project that named nothing cannot have its tests frozen, and freezing
// nothing while reporting success would be the worst possible answer.
func TestAProjectThatSaidNothingIsNotConfigured(t *testing.T) {
	if matcher(nil, nil).Configured() {
		t.Error("an empty configuration reported itself as configured")
	}
	if matcher([]string{" ", "/"}, []string{"  "}).Configured() {
		t.Error("whitespace counted as configuration")
	}
	if !matcher(nil, []string{"*_test.go"}).Configured() {
		t.Error("one pattern is configuration")
	}
}

func TestFilterKeepsTheTestsInOrder(t *testing.T) {
	m := matcher([]string{"tests/"}, []string{"*_test.go"})
	got := m.Filter([]string{"main.go", "tests/a.py", "internal/x_test.go", "README.md"})
	want := []string{"tests/a.py", "internal/x_test.go"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNothingIsNotATest(t *testing.T) {
	m := matcher([]string{"tests/"}, []string{"*_test.go"})
	for _, p := range []string{"", "  ", ".", "./"} {
		if m.Match(p) {
			t.Errorf("%q matched", p)
		}
	}
}

// What counts as a test has to be the same question on a case-insensitive
// filesystem, or the rules about test files quietly stop applying to a file
// somebody capitalised.
func TestWhatCountsAsATestIsNotACaseQuestion(t *testing.T) {
	m := New(config.TestPaths{FileGlobs: []string{"*_test.go"}, Dirs: []string{"spec"}})
	for _, path := range []string{
		"internal/invoice_test.go",
		"internal/Invoice_Test.go",
		"internal/INVOICE_TEST.GO",
		"spec/thing.rb",
		"Spec/thing.rb",
	} {
		if !m.Match(path) {
			t.Errorf("%s was not recognised as a test", path)
		}
	}
	if m.Match("internal/invoice.go") {
		t.Error("a source file was called a test")
	}
}

// A frozen test that reads a golden file is only frozen if the golden file is
// too. The same goes for a jest snapshot, a mock, and the conftest.py that
// decides what a pytest fixture returns: each one changes whether a test
// passes, without the test file being touched, and each was outside the
// freeze.
func TestWhatATestDependsOnIsATestToo(t *testing.T) {
	for _, c := range []struct {
		stack string
		paths config.TestPaths
		under []string
	}{
		{"Go",
			config.TestPaths{Dirs: []string{"testdata/"}, FileGlobs: []string{"*_test.go"}},
			[]string{"testdata/case1.txt", "internal/testdata/golden.json"}},
		{"Python",
			config.TestPaths{Dirs: []string{"tests/", "fixtures/"}, FileGlobs: []string{"test_*.py", "conftest.py"}},
			[]string{"conftest.py", "tests/conftest.py", "fixtures/rows.csv", "app/fixtures/rows.csv"}},
		{"Node",
			config.TestPaths{Dirs: []string{"__tests__/", "__snapshots__/", "__mocks__/"}, FileGlobs: []string{"*.test.ts", "*.snap"}},
			[]string{"src/__snapshots__/x.test.ts.snap", "src/__mocks__/api.ts", "src/__tests__/x.ts"}},
	} {
		t.Run(c.stack, func(t *testing.T) {
			m := New(c.paths)
			for _, p := range c.under {
				if !m.Match(p) {
					t.Errorf("%s is not covered by the freeze; a test's outcome can be changed there", p)
				}
			}
		})
	}
}

// A bare name is any directory of that name; one with a slash is that one
// directory. Without the first, `testdata/` covered only the top-level one and
// Go's own convention of internal/testdata went uncovered.
func TestABareDirectoryNameMatchesAtAnyDepth(t *testing.T) {
	m := New(config.TestPaths{Dirs: []string{"testdata", "src/fixtures"}})
	for _, p := range []string{"testdata/a", "deep/down/testdata/a", "src/fixtures/a"} {
		if !m.Match(p) {
			t.Errorf("%s did not match", p)
		}
	}
	for _, p := range []string{"other/fixtures/a", "testdata", "notestdata/a"} {
		if m.Match(p) {
			t.Errorf("%s matched and should not have", p)
		}
	}
}
