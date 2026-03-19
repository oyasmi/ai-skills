package apperr

import "fmt"

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
	if err != nil && As(err, &appErr) {
		return appErr.Code
	}
	return "internal_error"
}

func As(err error, target any) bool {
	type as interface {
		As(any) bool
	}
	if e, ok := err.(as); ok {
		return e.As(target)
	}
	switch t := target.(type) {
	case **Error:
		cur := err
		for cur != nil {
			if ae, ok := cur.(*Error); ok {
				*t = ae
				return true
			}
			type unwrapper interface{ Unwrap() error }
			u, ok := cur.(unwrapper)
			if !ok {
				return false
			}
			cur = u.Unwrap()
		}
	}
	return false
}
