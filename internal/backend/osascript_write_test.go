package backend

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

var eventStringFields = []struct {
	name    string
	set     func(*EventUpdateInput, *string)
	arg     int
	binding string
	setter  string
}{
	{"title", func(in *EventUpdateInput, s *string) { in.Title = s }, 3, `set titleText to item 4 of argv`, `set summary of targetRef to titleText`},
	{"location", func(in *EventUpdateInput, s *string) { in.Location = s }, 6, `set locText to item 7 of argv`, `set location of targetRef to locText`},
	{"notes", func(in *EventUpdateInput, s *string) { in.Notes = s }, 7, `set notesText to item 8 of argv`, `set description of targetRef to notesText`},
	{"url", func(in *EventUpdateInput, s *string) { in.URL = s }, 8, `set urlText to item 9 of argv`, `set url of targetRef to urlText`},
}

func TestBuildUpdateEventScriptStringValues(t *testing.T) {
	values := []struct{ name, value string }{
		{"empty", ""},
		{"sentinel", "__ACAL_KEEP__"},
		{"quotes", `say "hello" and 'goodbye'`},
		{"backslashes", `C:\calendar\notes`},
		{"tab", "before\tafter"},
		{"newline", "first\nsecond\r\nthird"},
		{"unicode", "Καλημέρα 日本語 📅"},
		{"edge spaces", "  unchanged  "},
	}
	for _, field := range eventStringFields {
		t.Run(field.name, func(t *testing.T) {
			empty := ""
			var in EventUpdateInput
			field.set(&in, &empty)
			baseline, _ := buildUpdateEventScript("event-uid", 0, ScopeSeries, in)
			for _, value := range values {
				t.Run(value.name, func(t *testing.T) {
					field.set(&in, &value.value)
					lines, args := buildUpdateEventScript("event-uid", 0, ScopeSeries, in)
					if len(args) != 13 {
						t.Fatalf("argv length = %d, want 13", len(args))
					}
					if args[field.arg] != value.value {
						t.Errorf("argv[%d] = %q, want %q", field.arg, args[field.arg], value.value)
					}
					if !reflect.DeepEqual(lines, baseline) {
						t.Error("string value changed script source instead of only argv")
					}
					script := "\n" + strings.Join(lines, "\n") + "\n"
					for _, want := range []string{field.binding, field.setter} {
						if strings.Count(script, "\n"+want+"\n") != 1 {
							t.Errorf("expected exactly one unconditional line %q", want)
						}
					}
				})
			}
		})
	}
}

func TestBuildUpdateEventScriptFieldCombinations(t *testing.T) {
	start := time.Unix(1700000000, 0)
	end := start.Add(time.Hour)
	allDay := false
	repeatRule := " WEEKLY "
	reminder := -15 * time.Minute
	values := []string{"__ACAL_KEEP__", "", "notes\nwith\tspacing", `https://example.com/"quoted"`}
	// Every subset checks omission independently of the contents of supplied fields.
	for mask := 0; mask < 1<<len(eventStringFields); mask++ {
		name := make([]string, 0, len(eventStringFields))
		in := EventUpdateInput{Start: &start, End: &end, AllDay: &allDay, RepeatRule: &repeatRule, ReminderOffset: &reminder, ClearReminder: true}
		wantArgs := []string{"event-uid", "future", "1700000000", "", "1700000000", "1700003600", "", "", "", "false", "weekly", "-15", "true"}
		wantSetters := []string{}
		for i, field := range eventStringFields {
			if mask&(1<<i) != 0 {
				name = append(name, field.name)
				field.set(&in, &values[i])
				wantArgs[field.arg] = values[i]
				wantSetters = append(wantSetters, field.setter)
			}
		}
		t.Run(strings.Join(name, "+"), func(t *testing.T) {
			lines, args := buildUpdateEventScript("event-uid", start.Unix()-cocoaEpochOffset, ScopeFuture, in)
			if !reflect.DeepEqual(args, wantArgs) {
				t.Errorf("argv = %#v, want %#v", args, wantArgs)
			}
			script := strings.Join(lines, "\n")
			before := "set targetRef to contents of targetEvent\n"
			after := `if startText is not "__ACAL_KEEP__" then set start date of targetRef to (epoch + (startText as integer))`
			wantBlock := before
			if len(wantSetters) > 0 {
				wantBlock += strings.Join(wantSetters, "\n") + "\n"
			}
			wantBlock += after
			if !strings.Contains(script, wantBlock) {
				t.Errorf("missing ordered setter block:\n%s", wantBlock)
			}
			for i, field := range eventStringFields {
				wantCount := 0
				if mask&(1<<i) != 0 {
					wantCount = 1
				}
				if got := strings.Count(script, field.setter); got != wantCount {
					t.Errorf("%s setter count = %d, want %d", field.name, got, wantCount)
				}
			}
		})
	}
}
