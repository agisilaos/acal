// Package nativeproof runs the experimental packaged EventKit integration.
// It is deliberately separate from production backend and history contracts.
package nativeproof

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

const Protocol = "acal-native-proof-v1"

type Request struct {
	Protocol  string         `json:"protocol"`
	RequestID string         `json:"request_id"`
	Operation string         `json:"operation"`
	Token     string         `json:"token,omitempty"`
	Args      map[string]any `json:"args"`
}
type Failure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type Response struct {
	Protocol  string          `json:"protocol"`
	RequestID string          `json:"request_id"`
	Outcome   string          `json:"outcome"`
	Data      json.RawMessage `json:"data,omitempty"`
	Error     *Failure        `json:"error,omitempty"`
}
type Record struct {
	Request Request   `json:"request"`
	Started time.Time `json:"started"`
	Result  *Response `json:"result,omitempty"`
}
type Client struct {
	Helper   string
	StateDir string
}

func Installed() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	p, err = filepath.EvalSymlinks(p)
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(p), "..", "libexec", "acal-native.app", "Contents", "MacOS", "acal-native"), nil
}
func id() (string, error) {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	return hex.EncodeToString(b), nil
}
func IsWrite(op string) bool {
	switch op {
	case "add", "update", "remind", "delete":
		return true
	}
	return false
}

func (c Client) Call(ctx context.Context, op string, args map[string]any) (Response, error) {
	rid, err := id()
	if err != nil {
		return Response{}, err
	}
	req := Request{Protocol: Protocol, RequestID: rid, Operation: op, Args: args}
	var record *Record
	if IsWrite(op) {
		if !filepath.IsAbs(c.StateDir) {
			return Response{}, errors.New("native writes require an absolute --state-dir for isolated fixture ownership and operation records")
		}
		if err = mkdirDurable(c.StateDir); err != nil {
			return Response{}, err
		}
		if err = os.Chmod(c.StateDir, 0700); err != nil {
			return Response{}, err
		}
		lock, e := os.OpenFile(filepath.Join(c.StateDir, "writer.lock"), os.O_CREATE|os.O_RDWR, 0600)
		if e != nil {
			return Response{}, e
		}
		defer lock.Close()
		if e = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
			return Response{}, errors.New("native proof writer is busy")
		}
		defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		tokenPath := filepath.Join(c.StateDir, "fixture-token")
		token, e := os.ReadFile(tokenPath)
		if os.IsNotExist(e) {
			v, x := id()
			if x != nil {
				return Response{}, x
			}
			token = []byte(v)
			e = atomicWrite(tokenPath, token)
		}
		if e != nil {
			return Response{}, e
		}
		if len(token) != 32 {
			return Response{}, errors.New("invalid native fixture token")
		}
		req.Token = string(token)
		record = &Record{Request: req, Started: time.Now().UTC()}
		if err = writeRecord(c.StateDir, record); err != nil {
			return Response{}, fmt.Errorf("no native write started: cannot persist intent: %w", err)
		}
	}
	response, callErr := c.run(ctx, req)
	if record != nil {
		record.Result = &response
		if err = writeRecord(c.StateDir, record); err != nil {
			message := "native operation may have completed; cannot persist result; inspect journal before retrying"
			if response.Outcome == "verified" {
				response.Outcome = "applied_unverified"
			}
			response.Error = &Failure{Code: "JOURNAL_FAILURE", Message: message}
			return response, fmt.Errorf("%s: %w", message, err)
		}
	}
	return response, callErr
}

// Cap protocol output so a broken helper cannot exhaust the CLI's memory.
type boundedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.limit {
		return 0, errors.New("helper output exceeded limit")
	}
	return b.Buffer.Write(p)
}
func (c Client) run(ctx context.Context, req Request) (Response, error) {
	outcome := "rejected"
	if IsWrite(req.Operation) {
		outcome = "unknown"
	}
	failure := func(code, msg string) (Response, error) {
		return Response{Protocol: Protocol, RequestID: req.RequestID, Outcome: outcome, Error: &Failure{code, msg}}, errors.New(msg)
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return failure("PROTOCOL_ERROR", err.Error())
	}
	cmd := exec.CommandContext(ctx, c.Helper)
	cmd.Stdin = bytes.NewReader(payload)
	stdout := &boundedBuffer{limit: 8 << 20}
	stderr := &boundedBuffer{limit: 16 << 10}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err = cmd.Start(); err != nil {
		outcome = "rejected"
		return failure("HELPER_UNAVAILABLE", "packaged native helper could not start; install the native proof archive")
	}
	err = cmd.Wait()
	if ctx.Err() != nil {
		return failure("TIMEOUT", "native helper interrupted; inspect operation records before retrying a write")
	}
	if err != nil {
		return failure("HELPER_FAILED", "native helper failed; inspect operation records before retrying a write")
	}
	var res Response
	if json.Unmarshal(stdout.Bytes(), &res) != nil || res.Protocol != Protocol || res.RequestID != req.RequestID {
		return failure("PROTOCOL_ERROR", "invalid or incompatible native helper response")
	}
	if res.Outcome != "verified" && res.Outcome != "rejected" && res.Outcome != "unknown" && res.Outcome != "applied_unverified" {
		return failure("PROTOCOL_ERROR", "invalid native outcome")
	}
	if res.Error != nil {
		return res, errors.New(res.Error.Message)
	}
	if res.Outcome != "verified" || len(res.Data) == 0 {
		return failure("PROTOCOL_ERROR", "missing native result")
	}
	return res, nil
}
func writeRecord(dir string, rec *Record) error {
	b, e := json.MarshalIndent(rec, "", "  ")
	if e != nil {
		return e
	}
	return atomicWrite(filepath.Join(dir, rec.Request.RequestID+".json"), append(b, '\n'))
}
func atomicWrite(path string, b []byte) error {
	f, e := os.CreateTemp(filepath.Dir(path), ".native-record-")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, e = f.Write(b); e != nil {
		return e
	}
	if e = f.Sync(); e != nil {
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	if e = os.Rename(f.Name(), path); e != nil {
		return e
	}
	d, e := os.Open(filepath.Dir(path))
	if e != nil {
		return e
	}
	defer d.Close()
	return d.Sync()
}

// Persist newly created directory entries before any Calendar mutation can start.
func mkdirDurable(dir string) error {
	var missing []string
	for p := dir; ; p = filepath.Dir(p) {
		_, err := os.Stat(p)
		if err == nil {
			break
		}
		if !os.IsNotExist(err) {
			return err
		}
		missing = append(missing, p)
		if filepath.Dir(p) == p {
			return err
		}
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	for _, p := range missing {
		parent, err := os.Open(filepath.Dir(p))
		if err != nil {
			return err
		}
		err = parent.Sync()
		closeErr := parent.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}
