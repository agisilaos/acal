package backend

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func stubCreation(t *testing.T, output string, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	count := filepath.Join(dir, "calls")
	t.Setenv("ACAL_CREATE_TEST_COUNT", count)
	t.Setenv("ACAL_CREATE_TEST_OUTPUT", output)
	t.Setenv("ACAL_CREATE_TEST_EXIT", strconv.Itoa(exitCode))
	t.Setenv("ACAL_OSASCRIPT_RETRIES", "3")
	t.Setenv("ACAL_OSASCRIPT_RETRY_BACKOFF", "1ms")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	// Capture the emitted source when requested; never execute Calendar calls.
	script := `#!/bin/sh
printf x >> "$ACAL_CREATE_TEST_COUNT"
if [ -n "$ACAL_CREATE_TEST_SOURCE" ]; then
  while [ "$#" -gt 0 ]; do
    case "$1" in
      -e) printf '%s\n' "$2" >> "$ACAL_CREATE_TEST_SOURCE"; shift 2 ;;
      --) break ;;
      *) shift ;;
    esac
  done
fi
printf '%s\n' "$ACAL_CREATE_TEST_OUTPUT"
exit "$ACAL_CREATE_TEST_EXIT"
`
	if err := os.WriteFile(filepath.Join(dir, "osascript"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return count
}

func creationTestInput() EventCreateInput {
	start := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	return EventCreateInput{Calendar: "Work", Title: "Review", Start: start, End: start.Add(time.Hour)}
}

func assertSingleCreationInvocation(t *testing.T, count string) {
	t.Helper()
	raw, err := os.ReadFile(count)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "x" {
		t.Fatalf("creation attempts=%q, want one", raw)
	}
}

func TestCreationDoesNotRetryNativeMutation(t *testing.T) {
	count := stubCreation(t, "AppleEvent timed out (-1712)", 1)
	item, err := NewOsaScriptBackend().AddEvent(context.Background(), creationTestInput())
	if item != nil || err == nil {
		t.Fatalf("item=%v error=%v", item, err)
	}
	assertSingleCreationInvocation(t, count)
}

func TestCreationLaunchedFailuresAreUnknown(t *testing.T) {
	for _, message := range []string{
		"Calendar got an error: AppleEvent timed out. (-1712)",
		"Connection is invalid",
		"Calendar got an error: Can't set description of event",
	} {
		t.Run(message, func(t *testing.T) {
			count := stubCreation(t, message, 23)
			item, err := NewOsaScriptBackend().AddEvent(context.Background(), creationTestInput())
			var outcome *CreationOutcomeError
			var commandErr *appleScriptCommandError
			var exitErr *exec.ExitError
			if item != nil || !errors.As(err, &outcome) || !errors.As(err, &commandErr) || !commandErr.Started || !errors.As(err, &exitErr) || exitErr.ExitCode() != 23 {
				t.Fatalf("item=%v error=%v command=%+v exit=%v", item, err, commandErr, exitErr)
			}
			if commandErr.Message != message || !strings.Contains(err.Error(), message) {
				t.Fatalf("native diagnostic lost: %v", err)
			}
			assertSingleCreationInvocation(t, count)
		})
	}
}

func TestCreationInvalidResultsAreUnknown(t *testing.T) {
	for _, result := range []string{"", " \t", "ACAL_CREATE_REJECTED:", "ACAL_CREATE_REJECTED:unexpected", "ACAL_WRITE_REJECTED:permission:extra"} {
		t.Run(result, func(t *testing.T) {
			count := stubCreation(t, result, 0)
			item, err := NewOsaScriptBackend().AddEvent(context.Background(), creationTestInput())
			var outcome *CreationOutcomeError
			if item != nil || !errors.As(err, &outcome) {
				t.Fatalf("invalid result %q: item=%v error=%v", result, item, err)
			}
			assertSingleCreationInvocation(t, count)
		})
	}
}

func TestCreationPreWriteMarkersAreDefinite(t *testing.T) {
	for _, reason := range []string{"calendar_not_found", "permission"} {
		t.Run(reason, func(t *testing.T) {
			result := "ACAL_CREATE_REJECTED:calendar_not_found"
			if reason == "permission" {
				result = "ACAL_WRITE_REJECTED:permission"
			}
			count := stubCreation(t, result, 0)
			in := creationTestInput()
			offset := -15 * time.Minute
			in.ReminderOffset = &offset
			item, err := NewOsaScriptBackend().AddEvent(context.Background(), in)
			var outcome *CreationOutcomeError
			var rejected *WriteRejectedError
			if item != nil || err == nil || errors.As(err, &outcome) {
				t.Fatalf("item=%v error=%v", item, err)
			}
			if reason == "permission" {
				if !errors.As(err, &rejected) || rejected.Reason != reason {
					t.Fatalf("permission classification lost: %v", err)
				}
			} else if err.Error() != "calendar not found" || errors.As(err, &rejected) {
				t.Fatalf("missing calendar classification changed: %v", err)
			}
			assertSingleCreationInvocation(t, count)
		})
	}
}

func TestCreationLaunchFailuresAreDefinite(t *testing.T) {
	for _, kind := range []string{"missing", "start"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("PATH", dir)
			if kind == "start" {
				if err := os.WriteFile(filepath.Join(dir, "osascript"), []byte("invalid executable format\n"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			item, err := NewOsaScriptBackend().AddEvent(context.Background(), creationTestInput())
			var outcome *CreationOutcomeError
			var commandErr *appleScriptCommandError
			if item != nil || err == nil || errors.As(err, &outcome) || !errors.As(err, &commandErr) || commandErr.Started {
				t.Fatalf("item=%v error=%v command=%+v", item, err, commandErr)
			}
			if kind == "missing" {
				var launchErr *exec.Error
				if !errors.As(err, &launchErr) || !errors.Is(err, exec.ErrNotFound) {
					t.Fatalf("missing-executable cause lost: %v", err)
				}
			} else {
				var launchErr *os.PathError
				if !errors.As(err, &launchErr) {
					t.Fatalf("start-failure cause lost: %v", err)
				}
			}
		})
	}
}

type creationInterruptedContext struct {
	context.Context
	failure error
}

func (c creationInterruptedContext) Err() error {
	if c.Context.Err() != nil {
		return c.failure
	}
	return nil
}

func TestCreationInterruptedProcessPreservesCauses(t *testing.T) {
	for _, failure := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(failure.Error(), func(t *testing.T) {
			dir := t.TempDir()
			ready := filepath.Join(dir, "ready")
			t.Setenv("ACAL_CREATE_TEST_READY", ready)
			t.Setenv("PATH", dir)
			// The readiness file proves the child started before interruption.
			script := "#!/bin/sh\nprintf ready > \"$ACAL_CREATE_TEST_READY\"\nwhile :; do :; done\n"
			if err := os.WriteFile(filepath.Join(dir, "osascript"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			ctx := creationInterruptedContext{Context: parent, failure: failure}
			type result struct {
				created bool
				err     error
			}
			finished := make(chan result, 1)
			go func() {
				item, err := NewOsaScriptBackend().AddEvent(ctx, creationTestInput())
				finished <- result{created: item != nil, err: err}
			}()
			deadline := time.Now().Add(5 * time.Second)
			for {
				if contents, err := os.ReadFile(ready); err == nil && string(contents) == "ready" {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("creation stub did not start")
				}
				time.Sleep(time.Millisecond)
			}
			cancel()
			select {
			case got := <-finished:
				var outcome *CreationOutcomeError
				var commandErr *appleScriptCommandError
				var exitErr *exec.ExitError
				if got.created || !errors.As(got.err, &outcome) || !errors.Is(got.err, failure) || !errors.As(got.err, &commandErr) || !commandErr.Started || !errors.As(got.err, &exitErr) {
					t.Fatalf("created=%t error=%v command=%+v exit=%v", got.created, got.err, commandErr, exitErr)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("interrupted creation did not terminate")
			}
		})
	}
}

func TestCreationScriptCompiles(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("native AppleScript compiler requires macOS")
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "creation.applescript")
	t.Setenv("ACAL_CREATE_TEST_SOURCE", source)
	count := stubCreation(t, "ACAL_CREATE_REJECTED:calendar_not_found", 0)
	_, err := NewOsaScriptBackend().AddEvent(context.Background(), creationTestInput())
	if err == nil || err.Error() != "calendar not found" {
		t.Fatalf("source capture failed: %v", err)
	}
	assertSingleCreationInvocation(t, count)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Compile the exact source supplied to osascript; never execute it.
	out, err := exec.CommandContext(ctx, "/usr/bin/osacompile", "-o", filepath.Join(dir, "creation.scpt"), source).CombinedOutput()
	if err != nil {
		t.Fatalf("creation script failed to compile: %v: %s", err, out)
	}
}
