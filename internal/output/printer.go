package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/agis/acal/internal/contract"
)

type Mode string

const (
	ModeAuto  Mode = "auto"
	ModeJSON  Mode = "json"
	ModeJSONL Mode = "jsonl"
	ModePlain Mode = "plain"
)

type Printer struct {
	Mode          Mode
	Command       string
	Fields        []string
	Quiet         bool
	NoColor       bool
	SchemaVersion string
	Out           io.Writer
	Err           io.Writer
}

func (p Printer) Success(data any, meta map[string]any, warnings []string) error {
	switch p.Mode {
	case ModeJSON:
		env := contract.SuccessEnvelope{
			SchemaVersion: p.schemaVersion(),
			Command:       p.Command,
			GeneratedAt:   time.Now().UTC(),
			Data:          data,
			Meta:          meta,
			Warnings:      warnings,
		}
		enc := json.NewEncoder(p.outWriter())
		enc.SetIndent("", "  ")
		return enc.Encode(env)
	case ModeJSONL:
		v := reflect.ValueOf(data)
		if v.IsValid() && v.Kind() == reflect.Slice {
			enc := json.NewEncoder(p.outWriter())
			for i := 0; i < v.Len(); i++ {
				if err := enc.Encode(v.Index(i).Interface()); err != nil {
					return err
				}
			}
			return nil
		}
		return json.NewEncoder(p.outWriter()).Encode(data)
	default:
		return p.printPlain(data)
	}
}

func (p Printer) Error(code contract.ErrorCode, message, hint string) error {
	if p.Mode == ModeJSON || p.Mode == ModeJSONL {
		env := contract.ErrorEnvelope{
			SchemaVersion: p.schemaVersion(),
			Error:         contract.ErrorBody{Code: code, Message: message, Hint: hint},
		}
		enc := json.NewEncoder(p.errWriter())
		enc.SetIndent("", "  ")
		return enc.Encode(env)
	}
	if hint != "" {
		_, _ = fmt.Fprintf(p.errWriter(), "%s: %s\nhint: %s\n", p.errorLabel(), message, hint)
		return nil
	}
	_, _ = fmt.Fprintf(p.errWriter(), "%s: %s\n", p.errorLabel(), message)
	return nil
}

func (p Printer) schemaVersion() string {
	if p.SchemaVersion == "" {
		return contract.SchemaVersion
	}
	return p.SchemaVersion
}

func (p Printer) printPlain(data any) error {
	v := reflect.ValueOf(data)
	if !v.IsValid() || (v.Kind() == reflect.Slice && v.Len() == 0) {
		if !p.Quiet {
			_, _ = fmt.Fprintln(p.outWriter(), "no results")
		}
		return nil
	}
	if v.Kind() == reflect.Slice {
		for i := 0; i < v.Len(); i++ {
			if _, err := fmt.Fprintln(p.outWriter(), flatten(v.Index(i).Interface(), p.Fields)); err != nil {
				return err
			}
		}
		return nil
	}
	_, err := fmt.Fprintln(p.outWriter(), flatten(data, p.Fields))
	return err
}

func (p Printer) outWriter() io.Writer {
	if p.Out != nil {
		return p.Out
	}
	return os.Stdout
}

func (p Printer) errWriter() io.Writer {
	if p.Err != nil {
		return p.Err
	}
	return os.Stderr
}

func (p Printer) errorLabel() string {
	if p.colorsEnabled() {
		return "\x1b[31merror\x1b[0m"
	}
	return "error"
}

func (p Printer) colorsEnabled() bool {
	if p.NoColor {
		return false
	}
	if strings.TrimSpace(os.Getenv("NO_COLOR")) != "" {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("TERM")), "dumb") {
		return false
	}
	if f, ok := p.errWriter().(*os.File); ok {
		if info, err := f.Stat(); err == nil {
			return info.Mode()&os.ModeCharDevice != 0
		}
	}
	return false
}

func flatten(v any, fields []string) string {
	if len(fields) == 0 {
		b, _ := json.Marshal(v)
		return string(b)
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		b, _ := json.Marshal(v)
		return string(b)
	}
	parts := make([]string, 0, len(fields))
	for _, f := range fields {
		fv := rv.FieldByNameFunc(func(name string) bool {
			return strings.EqualFold(name, strings.ReplaceAll(f, "_", "")) || strings.EqualFold(name, f)
		})
		if !fv.IsValid() {
			parts = append(parts, "")
			continue
		}
		parts = append(parts, fmt.Sprint(fv.Interface()))
	}
	return strings.Join(parts, "\t")
}
