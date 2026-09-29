package nativeproof

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if mode := os.Getenv("ACAL_PROOF_TEST_HELPER"); mode != "" {
		var req Request
		if json.NewDecoder(os.Stdin).Decode(&req) != nil {
			os.Exit(2)
		}
		if mode == "sleep" {
			time.Sleep(5 * time.Second)
		}
		if mode == "intent" {
			raw, e := os.ReadFile(filepath.Join(os.Getenv("ACAL_PROOF_TEST_STATE"), req.RequestID+".json"))
			if e != nil {
				os.Exit(3)
			}
			var r Record
			if json.Unmarshal(raw, &r) != nil || r.Result != nil || r.Request.Token == "" {
				os.Exit(4)
			}
		}
		if mode == "malformed" {
			os.Stdout.WriteString("not json")
			os.Exit(0)
		}
		res := Response{Protocol: Protocol, RequestID: req.RequestID, Outcome: "verified", Data: json.RawMessage(`{"id":"fixture"}`)}
		if mode == "mismatch" {
			res.RequestID = "other"
		}
		if mode == "rejected" {
			res.Outcome = "rejected"
			res.Data = nil
			res.Error = &Failure{"NOT_OWNED", "not owned"}
		}
		json.NewEncoder(os.Stdout).Encode(res)
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func client(t *testing.T, mode string) Client {
	t.Helper()
	t.Setenv("ACAL_PROOF_TEST_HELPER", mode)
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	state := t.TempDir()
	t.Setenv("ACAL_PROOF_TEST_STATE", state)
	return Client{Helper: exe, StateDir: state}
}
func TestIntentPrecedesMutationAndResultIsDurable(t *testing.T) {
	c := client(t, "intent")
	res, e := c.Call(context.Background(), "add", map[string]any{"title": "fixture"})
	if e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile(filepath.Join(c.StateDir, res.RequestID+".json"))
	if e != nil {
		t.Fatal(e)
	}
	var r Record
	if json.Unmarshal(raw, &r) != nil || r.Result == nil || r.Result.Outcome != "verified" {
		t.Fatalf("bad record: %s", raw)
	}
	info, _ := os.Stat(filepath.Join(c.StateDir, res.RequestID+".json"))
	if info.Mode().Perm() != 0600 {
		t.Fatal("record not private")
	}
}
func TestUncertainHelperResultsNeverBecomeKnownRejections(t *testing.T) {
	for _, mode := range []string{"malformed", "mismatch", "sleep"} {
		t.Run(mode, func(t *testing.T) {
			c := client(t, mode)
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			res, e := c.Call(ctx, "delete", map[string]any{"id": "fixture"})
			if e == nil || res.Outcome != "unknown" {
				t.Fatalf("%+v %v", res, e)
			}
			raw, _ := os.ReadFile(filepath.Join(c.StateDir, res.RequestID+".json"))
			var r Record
			json.Unmarshal(raw, &r)
			if r.Result == nil || r.Result.Outcome != "unknown" {
				t.Fatal("uncertainty not recorded")
			}
		})
	}
}
func TestMissingHelperAndNativeRejectionAreKnownBeforeMutation(t *testing.T) {
	c := client(t, "rejected")
	res, e := c.Call(context.Background(), "delete", nil)
	if e == nil || res.Outcome != "rejected" || res.Error.Code != "NOT_OWNED" {
		t.Fatalf("%+v %v", res, e)
	}
	c.Helper = filepath.Join(t.TempDir(), "absent")
	res, e = c.Call(context.Background(), "add", nil)
	if e == nil || res.Outcome != "rejected" {
		t.Fatalf("%+v %v", res, e)
	}
}
func TestWriteRequiresIsolatedStateAndSerializesWriters(t *testing.T) {
	c := client(t, "intent")
	c.StateDir = "relative"
	if _, e := c.Call(context.Background(), "add", nil); e == nil {
		t.Fatal("relative state accepted")
	}
	c.StateDir = t.TempDir()
	f, e := os.OpenFile(filepath.Join(c.StateDir, "writer.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		t.Fatal(e)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	if _, e = c.Call(context.Background(), "add", nil); e == nil {
		t.Fatal("concurrent writer accepted")
	}
	files, _ := filepath.Glob(filepath.Join(c.StateDir, "*.json"))
	if len(files) != 0 {
		t.Fatal("intent created while lock held")
	}
}

func TestNestedStateCreatedBeforeIntentAndWrite(t *testing.T) {
	c := client(t, "intent")
	c.StateDir = filepath.Join(c.StateDir, "new", "nested")
	t.Setenv("ACAL_PROOF_TEST_STATE", c.StateDir)
	if _, err := c.Call(context.Background(), "add", nil); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(c.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0700 {
		t.Fatal("state directory not private")
	}
}
