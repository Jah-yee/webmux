package archtest

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestPersistenceScanRules(t *testing.T) {
	tests := []struct {
		name, source, key string
		count             int
		fail              bool
	}{
		{"alias", `package p; import disk "os"; func save(){disk.WriteFile("x",nil,0600)}`, "internal/p/a.go|save|os.WriteFile", 1, false},
		{"function value", `package p; import "os"; var writer=os.WriteFile`, "internal/p/a.go|<package>|os.WriteFile", 1, false},
		{"new filename", `package p; const name="new.json"`, "internal/p/a.go|path|new.json", 1, false},
		{"new lock", `package p; const name="new.json.lock"`, "internal/p/a.go|path|new.json.lock", 1, false},
		{"duplicate calls", `package p; import "os"; func save(){os.Create("a");os.Create("b")}`, "internal/p/a.go|save|os.Create", 2, false},
		{"receiver", `package p; import "os"; type Store struct{};func(s *Store)save(){os.Create("a")}`, "internal/p/a.go|*Store.save|os.Create", 1, false},
		{"statefile alias", `package p; import sf "github.com/rainchan/webmux/internal/statefile"; var writer=sf.WriteJSON`, "internal/p/a.go|<package>|github.com/rainchan/webmux/internal/statefile.WriteJSON", 1, false},
		{"dot import", `package p; import . "os"; var writer=WriteFile`, "", 0, true},
		{"invalid syntax", `package`, "", 0, true},
		{"comment ignored", `package p; // os.WriteFile("ignored.json",nil,0600)`, "", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := persistenceScan(fstest.MapFS{"internal/p/a.go": {Data: []byte(tt.source)}})
			if (err != nil) != tt.fail {
				t.Fatalf("error = %v", err)
			}
			if !tt.fail && got[tt.key] != tt.count {
				t.Fatalf("got %v, expected %s=%d", got, tt.key, tt.count)
			}
		})
	}
	t.Run("platforms and scope", func(t *testing.T) {
		tree := fstest.MapFS{
			"internal/p/a_windows.go":  {Data: []byte("//go:build windows\n\npackage p;const p=\"windows.json\"")},
			"cmd/tool/main.go":         {Data: []byte("package main;const p=\"command.json\"")},
			"internal/p/a_test.go":     {Data: []byte("broken ignored fixture")},
			"internal/p/testdata/a.go": {Data: []byte("broken ignored fixture")},
			"web/a.go":                 {Data: []byte("broken ignored fixture")},
		}
		got, err := persistenceScan(tree)
		if err != nil || len(got) != 2 {
			t.Fatalf("got %v, %v", got, err)
		}
	})
}

func TestPersistenceInventoryRules(t *testing.T) {
	store := persistenceStore{ID: "example", Path: "example.json", Lock: "example.json.lock", Owner: "internal/example", Failure: "unavailable", Web: "yes", Source: "internal/example/store.go"}
	refs := map[string]persistenceReference{
		"internal/example/store.go|path|example.json":      {1, "example"},
		"internal/example/store.go|path|example.json.lock": {1, "example"},
		"internal/example/store.go|save|os.WriteFile":      {1, "example"},
	}
	catalog := func() persistenceInventory {
		r := map[string]persistenceReference{}
		for k, v := range refs {
			r[k] = v
		}
		return persistenceInventory{Stores: []persistenceStore{store}, References: r, Exceptions: map[string]string{}}
	}
	actual := func() map[string]int {
		m := map[string]int{}
		for k, v := range refs {
			m[k] = v.Count
		}
		return m
	}
	doc := inventoryStart + "\n|" + strings.Join(persistenceRows([]persistenceStore{store}), "|\n|") + "|\n" + inventoryEnd
	tree := func() fstest.MapFS {
		return fstest.MapFS{"internal/example/store.go": {Data: []byte("package example")}, "docs/persistence.md": {Data: []byte(doc)}}
	}
	if errs := checkPersistence(tree(), actual(), catalog()); len(errs) != 0 {
		t.Fatal(errs)
	}
	tests := []struct {
		name, want string
		mutate     func(fstest.MapFS, map[string]int, *persistenceInventory)
	}{
		{"new writer", "observed", func(_ fstest.MapFS, a map[string]int, _ *persistenceInventory) {
			a["internal/new/store.go|save|os.WriteFile"] = 1
		}},
		{"new path", "observed", func(_ fstest.MapFS, a map[string]int, _ *persistenceInventory) {
			a["internal/example/store.go|path|forgotten.json"] = 1
		}},
		{"extra call in allowed function", "observed", func(_ fstest.MapFS, a map[string]int, _ *persistenceInventory) {
			a["internal/example/store.go|save|os.WriteFile"]++
		}},
		{"stale permission", "stale reference", func(_ fstest.MapFS, a map[string]int, _ *persistenceInventory) {
			delete(a, "internal/example/store.go|save|os.WriteFile")
		}},
		{"missing owner", "unregistered owner", func(_ fstest.MapFS, _ map[string]int, c *persistenceInventory) {
			c.References["internal/example/store.go|save|os.WriteFile"] = persistenceReference{1, "unknown"}
		}},
		{"missing source", "missing store source", func(f fstest.MapFS, _ map[string]int, _ *persistenceInventory) {
			delete(f, "internal/example/store.go")
		}},
		{"invented path", "unobserved store path", func(_ fstest.MapFS, _ map[string]int, c *persistenceInventory) { c.Stores[0].Path = "fiction.json" }},
		{"duplicate store", "duplicate store", func(_ fstest.MapFS, _ map[string]int, c *persistenceInventory) { c.Stores = append(c.Stores, store) }},
		{"empty reason", "invalid exception", func(_ fstest.MapFS, _ map[string]int, c *persistenceInventory) { c.Exceptions["escape"] = "" }},
		{"unused exception", "unused owner", func(_ fstest.MapFS, _ map[string]int, c *persistenceInventory) { c.Exceptions["escape"] = "unused" }},
		{"missing table row", "table differs", func(f fstest.MapFS, _ map[string]int, _ *persistenceInventory) {
			f["docs/persistence.md"].Data = []byte(inventoryStart + "\n" + inventoryEnd)
		}},
		{"changed failure policy", "table differs", func(f fstest.MapFS, _ map[string]int, _ *persistenceInventory) {
			f["docs/persistence.md"].Data = []byte(strings.ReplaceAll(doc, "unavailable", "reset"))
		}},
		{"missing marker", "markers", func(f fstest.MapFS, _ map[string]int, _ *persistenceInventory) {
			f["docs/persistence.md"].Data = []byte("no table")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, a, c := tree(), actual(), catalog()
			tt.mutate(f, a, &c)
			errs := checkPersistence(f, a, c)
			if !strings.Contains(strings.Join(errs, "\n"), tt.want) {
				t.Fatalf("want %q, got %v", tt.want, errs)
			}
		})
	}
}

func TestPersistenceDomainWritersCannotBeAllowlisted(t *testing.T) {
	for _, symbol := range []string{"os.WriteFile", "syscall.Flock", "github.com/rainchan/webmux/internal/statefile.WriteJSON"} {
		if len(checkDomainWriters(map[string]int{"internal/settings/new.go|save|" + symbol: 1})) != 1 {
			t.Fatalf("bypass accepted: %s", symbol)
		}
	}
	if len(checkRuntimeCatalog(persistenceInventory{})) == 0 {
		t.Fatal("missing descriptors accepted")
	}
}
