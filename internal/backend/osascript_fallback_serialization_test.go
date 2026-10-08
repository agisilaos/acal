package backend

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestFallbackReadsPreserveNativeEventText(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("native AppleScript serialization requires macOS")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, title, location, notes, url string
	}{
		{"metadata", "Review", "Room", "Bring the project plan", "https://example.com/a;b?x=1,2"},
		{"literal controls", "  Review\t\n", "Room\r4", "\tΚαλημέρα\n\"quoted\" \\ notes\x01\n", "https://example.com/"},
		{"empty optional fields", "Review", "", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			dbPath := filepath.Join(dir, "empty.db")
			if err := os.WriteFile(dbPath, nil, 0600); err != nil {
				t.Fatal(err)
			}
			useCalendarFixture(t, dbPath)
			fields, err := json.Marshal([]string{"uid", "cal-1", "Work", tc.title, "1791014400", "1791018000", "false", tc.location, tc.notes, tc.url})
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("ACAL_READ_FIXTURE", string(fields))
			// At the native boundary, execute the production field serialization
			// and helpers against literal fixture values. The Calendar fetch is
			// excluded, so this cannot read or mutate the user's calendars.
			driver := `import json, os, pathlib, subprocess, sys, tempfile
args = sys.argv[1:]
lines = [args[i + 1] for i, arg in enumerate(args[:-1]) if arg == "-e"]
# Compile the full production read script, without executing Calendar access.
with tempfile.TemporaryDirectory() as temporary:
    source_path = pathlib.Path(temporary) / "read.applescript"
    source_path.write_text("\n".join(lines))
    compiled = subprocess.run(["/usr/bin/osacompile", "-o", str(pathlib.Path(temporary) / "read.scpt"), str(source_path)], capture_output=True)
    if compiled.returncode:
        sys.stderr.buffer.write(compiled.stderr)
        sys.exit(compiled.returncode)
helpers = lines[:lines.index("on run argv")]
serialize = next(line for line in lines if line.startswith("set rowText to "))
names = ["evUID", "calID", "calName", "evTitle", "evStartUnix", "evEndUnix", "evAllDay", "evLoc", "evNotes", "evURL"]
source = helpers + ["on run argv"] + ["set " + name + " to item " + str(i + 1) + " of argv" for i, name in enumerate(names)] + [serialize, "return rowText", "end run"]
command = ["/usr/bin/osascript", "-s", "h"]
for line in source:
    command += ["-e", line]
command += ["--"] + json.loads(os.environ["ACAL_READ_FIXTURE"])
result = subprocess.run(command, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
sys.stdout.buffer.write(result.stdout)
sys.stderr.buffer.write(result.stderr)
sys.exit(result.returncode)
`
			driverPath := filepath.Join(dir, "fixture.py")
			if err := os.WriteFile(driverPath, []byte(driver), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("ACAL_READ_FIXTURE_DRIVER", driverPath)
			t.Setenv("ACAL_READ_FIXTURE_PYTHON", python)
			wrapper := "#!/bin/sh\nexec \"$ACAL_READ_FIXTURE_PYTHON\" \"$ACAL_READ_FIXTURE_DRIVER\" \"$@\"\n"
			if err := os.WriteFile(filepath.Join(dir, "osascript"), []byte(wrapper), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			event, err := NewOsaScriptBackend().GetEventByID(ctx, "uid@812707200")
			if err != nil {
				t.Fatal(err)
			}
			if event.Title != tc.title || event.Location != tc.location || event.Notes != tc.notes || event.URL != tc.url {
				t.Fatalf("fallback changed literal event fields: %+v", event)
			}
		})
	}
}
