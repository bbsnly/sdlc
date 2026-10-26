package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// AC-9 (ANALYSIS "AC-9 as it stands"): no test file names a person's
// account, including the six `./task no-internal-docs` refused when US-023
// landed. The walk reads every _test.go file under the module, tracked or
// not, so a test file a story adds is held before it is staged; the six are
// named so that a walk that finds none of them cannot pass.
func TestAC9NoTestFileInTheRepositoryNamesAPersonsAccount(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	for _, rel := range []string{
		"internal/cli/inclaudecode_test.go",
		"internal/policy/policy_test.go",
		"internal/shellpolicy/claudeconfig_test.go",
		"internal/shellpolicy/claudeconfigunits_test.go",
	} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("AC-9 %s, one of the files the criterion names, is not there: %v", rel, err)
		}
		for _, f := range accountFindings(root, filepath.FromSlash(rel)) {
			t.Errorf("AC-9 %s:%d: %s", f.file, f.line, f.msg)
		}
	}
	seen := 0
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", ".sdlc":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		seen++
		for _, f := range accountFindings(root, rel) {
			t.Errorf("AC-9 %s:%d: %s", f.file, f.line, f.msg)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if seen < 100 {
		t.Errorf("AC-9 the walk read %d test files; the module has hundreds, so it did not read the module", seen)
	}
}
