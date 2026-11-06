package sn

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSNFilePath_Workdir(t *testing.T) {
	for _, tt := range []struct{ name, workdir, want string }{
		{"default", "", filepath.Join("/var/lib/probecube-agent", "machine-id")},
		{"relative", "state", filepath.Join("state", "machine-id")},
		{"custom", t.TempDir(), ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			want := tt.want
			if want == "" {
				want = filepath.Join(tt.workdir, "machine-id")
			}
			if got := snFilePath(tt.workdir); got != want {
				t.Fatalf("path = %q, want %q", got, want)
			}
		})
	}
}

func TestLoad_ExistingFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "machine-id"), []byte(" saved-sn\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := Load(dir); got != "saved-sn" {
		t.Fatalf("SN = %q, want saved-sn", got)
	}
}

func TestLoad_CreatesDirectoryAndPersists(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "state")
	sn := Load(dir)
	if sn == "" {
		t.Fatal("empty SN")
	}
	data, err := os.ReadFile(filepath.Join(dir, "machine-id"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != sn {
		t.Fatalf("persisted SN = %q, want %q", data, sn)
	}
	if got := Load(dir); got != sn {
		t.Fatalf("reloaded SN = %q, want %q", got, sn)
	}
}

func TestLoad_UnwritableDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte("unchanged"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := Load(path); got == "" {
		t.Fatal("expected fallback SN")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "unchanged" {
		t.Fatalf("existing file changed: %q", data)
	}
}
