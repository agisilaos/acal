package app

import (
	"errors"
	"fmt"
)

type AppError struct {
	Code    int
	Err     error
	Printed bool
}

func (e AppError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("exit code %d", e.Code)
	}
	return e.Err.Error()
}

func Wrap(code int, err error) error {
	if err == nil {
		return nil
	}
	return AppError{Code: code, Err: err}
}

func WrapPrinted(code int, err error) error {
	if err == nil {
		return nil
	}
	return AppError{Code: code, Err: err, Printed: true}
}

func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var e AppError
	if errors.As(err, &e) {
		return e.Code
	}
	return 1
}
