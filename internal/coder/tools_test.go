package coder

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repo(t *testing.T) Repo {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"src/auth/signup.ts":  "export function signup(name: string) {\n  return save(name.trim())\n}\n",
		"src/index.ts":        "import { signup } from './auth/signup'\n",
		"node_modules/x/x.js": "signup()\n",
		"README.md":           "# Checkout\n",
	}
	for path, content := range files {
		full := filepath.Join(root, path)
		_ = os.MkdirAll(filepath.Dir(full), 0o755)
		_ = os.WriteFile(full, []byte(content), 0o644)
	}
	return Repo{Root: root}
}

func TestReadListSearch(t *testing.T) {
	r := repo(t)
	listed, err := r.ListFiles("")
	if err != nil || !strings.Contains(listed, "src/auth/") || strings.Contains(listed, "node_modules/x") {
		t.Fatalf("list: %v\n%s", err, listed)
	}
	if strings.Contains(listed, "signup.ts") {
		t.Fatalf("listing goes two levels deep only:\n%s", listed)
	}
	read, err := r.ReadFile("src/auth/signup.ts", 2, 1)
	if err != nil || !strings.HasPrefix(read, "    2    return save(name.trim())") {
		t.Fatalf("read: %v\n%q", err, read)
	}
	found, err := r.scan("signup", r.Root, "")
	if err != nil || !strings.Contains(found, "src/auth/signup.ts:1:") || strings.Contains(found, "node_modules") {
		t.Fatalf("search: %v\n%s", err, found)
	}
	if got, _ := r.Search(context.Background(), "nothing-like-this", "", ""); got != "(no matches)" {
		t.Fatalf("got %q", got)
	}
}

func TestPathsStayInsideTheRepository(t *testing.T) {
	r := repo(t)
	for _, path := range []string{"../../etc/passwd", "/etc/passwd"} {
		full, err := r.Path(path)
		if err == nil && !strings.HasPrefix(full, r.Root) {
			t.Fatalf("%s resolved outside: %s", path, full)
		}
	}
	outside := t.TempDir()
	_ = os.Symlink(outside, filepath.Join(r.Root, "escape"))
	if _, err := r.Path("escape/secret"); err != ErrOutside {
		t.Fatalf("a symlink out of the repository must be refused, got %v", err)
	}
}

func TestEditsReplaceExactlyOneMatch(t *testing.T) {
	r := repo(t)
	edit, err := r.PrepareEdit("src/auth/signup.ts", "name.trim()", "name.trim() || 'there'")
	if err != nil {
		t.Fatal(err)
	}
	if err := edit.Apply(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(r.Root, "src/auth/signup.ts"))
	if !strings.Contains(string(data), "name.trim() || 'there'") {
		t.Fatalf("not applied:\n%s", data)
	}
	if _, err := r.PrepareEdit("src/auth/signup.ts", "nope", "x"); err == nil {
		t.Fatal("a missing old_str must be refused")
	}
	_ = os.WriteFile(filepath.Join(r.Root, "twice.ts"), []byte("a\na\n"), 0o644)
	if _, err := r.PrepareEdit("twice.ts", "a", "b"); err == nil || !strings.Contains(err.Error(), "2 times") {
		t.Fatalf("got %v", err)
	}
	created, err := r.PrepareEdit("src/new/file.ts", "", "export {}\n")
	if err != nil || !created.Create || created.Apply() != nil {
		t.Fatalf("create: %v", err)
	}
}

func TestCommandsRunInTheRepository(t *testing.T) {
	r := repo(t)
	out := r.Run(context.Background(), "ls src && exit 3")
	if !strings.HasPrefix(out, "exit code 3\n") || !strings.Contains(out, "auth") {
		t.Fatalf("got %q", out)
	}
	if long := clip(strings.Repeat("x", 50_000)); len(long) > maxOutputChars+100 {
		t.Fatalf("output not clipped: %d", len(long))
	}
}
