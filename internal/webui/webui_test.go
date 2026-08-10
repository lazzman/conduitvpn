package webui

import (
	"testing"
	"testing/fstest"
)

func TestStaticAssetVersionStable(t *testing.T) {
	fsys := fstest.MapFS{
		"static/index.html": &fstest.MapFile{Data: []byte("index")},
		"static/login.html": &fstest.MapFile{Data: []byte("login")},
		"static/styles.css": &fstest.MapFile{Data: []byte("styles")},
		"static/app.js":     &fstest.MapFile{Data: []byte("app")},
	}

	first := staticAssetVersion(fsys)
	second := staticAssetVersion(fsys)
	if first != second {
		t.Fatalf("same assets produced different versions: %q != %q", first, second)
	}
	if len(first) != 16 {
		t.Fatalf("expected 8-byte hex version, got %q", first)
	}
}

func TestStaticAssetVersionChangesWithContent(t *testing.T) {
	fsys := fstest.MapFS{
		"static/index.html": &fstest.MapFile{Data: []byte("index")},
		"static/login.html": &fstest.MapFile{Data: []byte("login")},
		"static/styles.css": &fstest.MapFile{Data: []byte("styles")},
		"static/app.js":     &fstest.MapFile{Data: []byte("app")},
	}

	before := staticAssetVersion(fsys)
	fsys["static/app.js"] = &fstest.MapFile{Data: []byte("app changed")}
	after := staticAssetVersion(fsys)
	if before == after {
		t.Fatalf("asset content change did not change version: %q", before)
	}
}
