package shareddir

import (
	"os"
	"path/filepath"
	"testing"
)

func TestShouldSkipDefaults(t *testing.T) {
	f := NewFilter(t.TempDir())
	cases := map[string]bool{
		"main.go":                  false,
		"src/foo.txt":              false,
		".git/HEAD":                true,
		"node_modules/x/y.js":      true,
		"dist/bundle.js":           true,
		"package-lock.json":        false, // suffix is ".lock" not ".json"
		"yarn.lock":                true,
		"foo/bar.lock":             true,
		"__pycache__/foo.pyc":      true,
		"src/__pycache__/foo.pyc":  false, // matches as prefix only
	}
	for path, want := range cases {
		if got := f.ShouldSkip(path); got != want {
			t.Errorf("ShouldSkip(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestShouldSkipFromGitignore(t *testing.T) {
	dir := t.TempDir()
	gi := "secrets.env\n*.tmp\n# comment\n!keep.tmp\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(gi), 0o644); err != nil {
		t.Fatal(err)
	}
	f := NewFilter(dir)
	if !f.ShouldSkip("secrets.env") {
		t.Error("expected secrets.env to be skipped")
	}
	if !f.ShouldSkip("foo.tmp") {
		t.Error("expected *.tmp to be skipped")
	}
	if f.ShouldSkip("foo.txt") {
		t.Error("foo.txt should not be skipped")
	}
}

func TestValidatePath(t *testing.T) {
	bad := []string{
		"",
		"/abs",
		"foo/../etc/passwd",
		"../escape",
		"a/b/../c",
	}
	for _, p := range bad {
		if err := ValidatePath(p); err == nil {
			t.Errorf("expected error for %q", p)
		}
	}
	good := []string{"foo", "foo/bar", "deep/nested/file.txt", "a.b.c"}
	for _, p := range good {
		if err := ValidatePath(p); err != nil {
			t.Errorf("unexpected error for %q: %v", p, err)
		}
	}
}
