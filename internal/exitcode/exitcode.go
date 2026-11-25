package exitcode

import (
	"errors"
	"fmt"
)

const (
	CodeOK      = 0
	CodePartial = 1
	CodeConfig  = 2
)

type Error struct {
	Code int
	Err  error
}

func (e *Error) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func Config(err error) error {
	if err == nil {
		return nil
	}
	return &Error{Code: CodeConfig, Err: err}
}

func Configf(format string, args ...any) error {
	return &Error{Code: CodeConfig, Err: fmt.Errorf(format, args...)}
}

func Partial(err error) error {
	if err == nil {
		return nil
	}
	return &Error{Code: CodePartial, Err: err}
}

func Partialf(format string, args ...any) error {
	return &Error{Code: CodePartial, Err: fmt.Errorf(format, args...)}
}

func FromError(err error) int {
	if err == nil {
		return CodeOK
	}
	var coded *Error
	if errors.As(err, &coded) {
		return coded.Code
	}
	return CodePartial
}
