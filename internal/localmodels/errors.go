package localmodels

// errors.go carries the machine codes a local model failure reports next to its
// message.

import "errors"

// codedError is a failure with a machine code the dashboard shows.
type codedError struct {
	code string
	err  error
}

func (e *codedError) Error() string { return e.err.Error() }
func (e *codedError) Unwrap() error { return e.err }
func (e *codedError) Code() string  { return e.code }

// withCode attaches a machine code to err; nil stays nil.
func withCode(code string, err error) error {
	if err == nil {
		return nil
	}
	return &codedError{code: code, err: err}
}

// codeOf is the code an error carries, "" when it carries none.
func codeOf(err error) string {
	var coded interface{ Code() string }
	if errors.As(err, &coded) {
		return coded.Code()
	}
	return ""
}
