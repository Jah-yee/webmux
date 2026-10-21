package web

import (
	"bytes"
	"io/fs"
	"os"
	"path"
	"testing"
)

// Compare the Go ZIP reader against Vite's ordinary output. This covers both
// deflated JS/CSS and stored fonts/images, including lazy language chunks.
func TestEmbeddedAssetsMatchBuild(t *testing.T) {
	count := 0
	err := fs.WalkDir(os.DirFS("dist"), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || name == "embed.zip" || name == ".gitkeep" {
			return err
		}
		want, err := os.ReadFile(path.Join("dist", name))
		if err != nil {
			return err
		}
		got, err := fs.ReadFile(Assets, "dist/"+name)
		if err != nil {
			return err
		}
		if !bytes.Equal(got, want) {
			t.Errorf("embedded asset %s differs from build", name)
		}
		count++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatal("no built assets checked")
	}
	if _, err := Assets.Open("dist/embed.zip"); !os.IsNotExist(err) {
		t.Fatalf("archive contains itself: %v", err)
	}
}
