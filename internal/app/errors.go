package app

import "fmt"

type AppError struct {
	Code int
	Err  error
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

func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	if e, ok := err.(AppError); ok {
		return e.Code
	}
	return 1
}
