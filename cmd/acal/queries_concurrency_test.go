package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

func TestConcurrentQueryUpdates(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "acal-bin")
	if out, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	path := filepath.Join(dir, "acal", "queries.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	store := map[string]map[string]string{}
	for i := 0; i < 1000; i++ {
		name := fmt.Sprintf("seed-%d", i)
		store[name] = map[string]string{"name": name, "from": "today", "to": "+30d"}
	}
	raw, _ := json.Marshal(store)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "missing.toml"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "XDG_CONFIG_HOME="+dir, "ACAL_CONFIG="+filepath.Join(dir, "missing.toml"))
	var wg sync.WaitGroup
	done := make(chan struct{})
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			select {
			case <-done:
				return
			default:
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Error(err)
				return
			}
			var snapshot map[string]any
			if err := json.Unmarshal(raw, &snapshot); err != nil {
				t.Errorf("partial snapshot: %v", err)
				return
			}
		}
	}()
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			args := []string{"queries", "save", fmt.Sprintf("added-%d", i), "--json"}
			if i%2 == 0 {
				args = []string{"queries", "delete", fmt.Sprintf("seed-%d", i), "--json"}
			}
			cmd := exec.Command(binary, args...)
			cmd.Env = env
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("%v: %v %s", args, err, out)
			}
		}(i)
	}
	wg.Wait()
	close(done)
	<-readerDone
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	store = nil
	if err := json.Unmarshal(raw, &store); err != nil {
		t.Fatal(err)
	}
	if len(store) != 1000 {
		t.Fatalf("got %d presets, want 1000", len(store))
	}
	for i := 0; i < 1000; i++ {
		_, ok := store[fmt.Sprintf("seed-%d", i)]
		want := !(i < 40 && i%2 == 0)
		if ok != want {
			t.Errorf("seed %d present=%v", i, ok)
		}
	}
	for i := 1; i < 40; i += 2 {
		if _, ok := store[fmt.Sprintf("added-%d", i)]; !ok {
			t.Errorf("lost added-%d", i)
		}
	}
	// Rejected writes preserve every acknowledged update.
	before := string(raw)
	for _, args := range [][]string{{"queries", "save", "added-1", "--json"}, {"queries", "delete", "missing", "--json"}} {
		cmd := exec.Command(binary, args...)
		cmd.Env = env
		if err := cmd.Run(); err == nil {
			t.Fatalf("%v unexpectedly succeeded", args)
		}
	}
	raw, _ = os.ReadFile(path)
	if string(raw) != before {
		t.Fatal("rejected update changed the store")
	}
}
