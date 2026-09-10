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
