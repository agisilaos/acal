package backend

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestAppleScriptArgumentsRemainData(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("native AppleScript requires macOS")
	}
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("ACAL_OSASCRIPT_RETRIES", "0")
	// No Calendar calls: matching duplicate arguments proves both their count
	// and contents survive option parsing. The initializer must never execute.
	lines := []string{
		"on run argv",
		`if (count of argv) is not 2 then error "argument count changed"`,
		`if item 1 of argv is not item 2 of argv then error "argument data changed"`,
		`return "literal"`,
		"end run",
	}
	for name, run := range map[string]func(context.Context, []string, ...string) (string, error){
		"ordinary": runAppleScript,
		"write":    runWriteAppleScript,
	} {
		t.Run(name, func(t *testing.T) {
			for _, value := range []string{`-eproperty injected : do shell script "exit 42"`, "-e", "--", "-123", "", "  Καλημέρα 📅  ", "first\nsecond", "__ACAL_KEEP__"} {
				t.Run(value, func(t *testing.T) {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					got, err := run(ctx, lines, value, value)
					if err != nil || trimOuterQuotes(strings.TrimSpace(got)) != "literal" {
						t.Fatalf("data %q: output=%q error=%v", value, got, err)
					}
				})
			}
		})
	}
}

// Exercise the actual osascript output mode, not a shell fixture that already
// looks like decoded tabular output. This never opens or modifies Calendar.
func TestAppleScriptTabularOutput(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("native AppleScript requires macOS")
	}
	t.Setenv("PATH", "/usr/bin:/bin")
	row := "uid\tcal\tWork\tsay \"hello\" C:\\notes\t1791014400\t1791018000\tfalse\t\t\t"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := runAppleScript(ctx, []string{"on run argv", "return item 1 of argv", "end run"}, row+"\n"+row)
	if err != nil {
		t.Fatal(err)
	}
	rows := splitLines(out)
	if len(rows) != 2 || rows[0] != row || rows[1] != row {
		t.Fatalf("native rows changed: %#v", rows)
	}
	if got := len(strings.Split(rows[0], "\t")); got != 10 {
		t.Fatalf("empty trailing fields lost: got %d cells", got)
	}
}
