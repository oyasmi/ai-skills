package apperr

import (
	"errors"
	"fmt"
)

type Error struct {
	Code    string
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Code
}

func (e *Error) Unwrap() error {
	return e.Err
}

func New(code, message string) *Error {
	return &Error{Code: code, Message: message}
}

func Wrap(code string, err error, format string, args ...any) *Error {
	msg := fmt.Sprintf(format, args...)
	return &Error{Code: code, Message: msg, Err: err}
}

func Code(err error) string {
	var appErr *Error
	if err != nil && errors.As(err, &appErr) {
		return appErr.Code
	}
	return "internal_error"
}
