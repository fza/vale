package system

import (
	"archive/zip"
	"bytes"
	"io/fs"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestMemFS_ReadFile(t *testing.T) {
	m := NewMemFS()
	m.AddFile("/styles/Demo/Rule.yml", []byte("hello"))

	got, err := m.ReadFile("/styles/Demo/Rule.yml")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("got %q, want %q", got, "hello")
	}

	if _, merr := m.ReadFile("/missing"); merr == nil {
		t.Fatalf("expected error for missing path")
	}
}

func TestMemFS_StatAndIsDir(t *testing.T) {
	m := NewMemFS()
	m.AddFile("/styles/Demo/Rule.yml", []byte("x"))

	if !m.IsDir("/styles") {
		t.Fatalf("/styles should be a directory")
	}
	if !m.IsDir("/styles/Demo") {
		t.Fatalf("/styles/Demo should be a directory")
	}
	if m.IsDir("/styles/Demo/Rule.yml") {
		t.Fatalf("file reported as directory")
	}
	if !m.Exists("/styles/Demo/Rule.yml") {
		t.Fatalf("Exists returned false for present file")
	}
	if m.Exists("/nope") {
		t.Fatalf("Exists returned true for missing path")
	}
}

func TestMemFS_Walk(t *testing.T) {
	m := NewMemFS()
	m.AddFile("/styles/A/Rule.yml", []byte("a"))
	m.AddFile("/styles/A/Sub/Other.yml", []byte("b"))
	m.AddFile("/styles/B/Rule.yml", []byte("c"))

	var visited []string
	err := m.Walk("/styles", func(p string, info fs.FileInfo, err error) error {
		_ = info
		_ = err
		visited = append(visited, p)
		return nil
	})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	sort.Strings(visited)
	want := []string{
		"/styles",
		"/styles/A",
		"/styles/A/Rule.yml",
		"/styles/A/Sub",
		"/styles/A/Sub/Other.yml",
		"/styles/B",
		"/styles/B/Rule.yml",
	}
	if !reflect.DeepEqual(visited, want) {
		t.Fatalf("walk order mismatch\n got=%v\nwant=%v", visited, want)
	}
}

func TestMemFS_WalkSkipDir(t *testing.T) {
	m := NewMemFS()
	m.AddFile("/styles/A/Rule.yml", []byte("a"))
	m.AddFile("/styles/B/Rule.yml", []byte("b"))

	var visited []string
	err := m.Walk("/styles", func(p string, _ fs.FileInfo, _ error) error {
		visited = append(visited, p)
		if p == "/styles/A" {
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	// SkipDir leaves that directory's entries unvisited and carries on with
	// its siblings, as filepath.Walk does: a style's `_drafts` must not hide
	// the rules after it.
	want := []string{"/styles", "/styles/A", "/styles/B", "/styles/B/Rule.yml"}
	if !reflect.DeepEqual(visited, want) {
		t.Fatalf("visited %v, want %v", visited, want)
	}
}

// A relative root yields relative paths, so a caller can take them relative
// to the root it named -- which is how a rule gets its name from its place
// under the style.
func TestMemFS_WalkRelativeRoot(t *testing.T) {
	m := NewMemFS()
	m.AddFile("styles/A/Rule.yml", []byte("a"))
	m.AddFile("styles/A/sub/Nested.yml", []byte("b"))

	var visited []string
	err := m.Walk("styles/A", func(p string, _ fs.FileInfo, _ error) error {
		visited = append(visited, p)
		return nil
	})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	want := []string{"styles/A", "styles/A/Rule.yml", "styles/A/sub", "styles/A/sub/Nested.yml"}
	if !reflect.DeepEqual(visited, want) {
		t.Fatalf("visited %v, want %v", visited, want)
	}

	rel, err := filepath.Rel("styles/A", visited[3])
	if err != nil || rel != filepath.Join("sub", "Nested.yml") {
		t.Fatalf("Rel = %q, %v", rel, err)
	}
}

func TestMemFS_Glob(t *testing.T) {
	m := NewMemFS()
	m.AddFile("/dict/en_US.dic", []byte("a"))
	m.AddFile("/dict/en_US.aff", []byte("a"))
	m.AddFile("/dict/extra.dic", []byte("a"))
	m.AddFile("/dict/sub/nested.dic", []byte("a"))

	got, err := m.Glob("/dict/*.dic")
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	want := []string{"/dict/en_US.dic", "/dict/extra.dic"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Glob mismatch\n got=%v\nwant=%v", got, want)
	}
}

func TestMemFS_GlobRecursive(t *testing.T) {
	m := NewMemFS()
	m.AddFile("/styles/A/Rule.yml", []byte("a"))
	m.AddFile("/styles/config/ignore/words.txt", []byte("a"))
	m.AddFile("/styles/config/ignore/sub/more.txt", []byte("a"))

	got, err := m.GlobRecursive("/styles/config/ignore", "*.txt")
	if err != nil {
		t.Fatalf("GlobRecursive: %v", err)
	}
	want := []string{"/styles/config/ignore/sub/more.txt", "/styles/config/ignore/words.txt"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GlobRecursive mismatch\n got=%v\nwant=%v", got, want)
	}
}

func TestMemFS_AddZip(t *testing.T) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, body := range map[string]string{
		"Demo/A.yml":     "hello",
		"Demo/sub/B.yml": "world",
	} {
		f, err := w.Create(name)
		if err != nil {
			t.Fatalf("zip.Create: %v", err)
		}
		f.Write([]byte(body))
	}
	if err := w.Close(); err != nil {
		t.Fatalf("zip.Close: %v", err)
	}

	m := NewMemFS()
	if err := m.AddZip("/styles", buf.Bytes()); err != nil {
		t.Fatalf("AddZip: %v", err)
	}
	if got, _ := m.ReadFile("/styles/Demo/A.yml"); string(got) != "hello" {
		t.Fatalf("Demo/A.yml mismatch: %q", got)
	}
	if got, _ := m.ReadFile("/styles/Demo/sub/B.yml"); string(got) != "world" {
		t.Fatalf("Demo/sub/B.yml mismatch: %q", got)
	}
}
