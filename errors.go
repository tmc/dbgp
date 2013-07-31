package dbgp

import "fmt"

type dbgpError struct {
	Code    int    `xml:"code,attr"`
	Message string `xml:"message"`
}

func (e dbgpError) Error() string {
	return fmt.Sprintf("%s (%d)", e.Message, e.Code)
}

var (
	// ErrParseError means an error occurred while parsing
	ErrParseError = dbgpError{1, "Parse Error"}
	// ErrInvalidOpts means invalid options were supplied
	ErrInvalidOpts = dbgpError{3, "Invaild Options"}
	// ErrUnimplemented means the attempted action is not implemented
	ErrUnimplemented = dbgpError{4, "Unimplemented"}
)
