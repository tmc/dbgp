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
	ErrParseError    = dbgpError{1, "Parse Error"}
	ErrInvalidOpts   = dbgpError{3, "Invaild Options"}
	ErrUnimplemented = dbgpError{4, "Unimplemented"}
)
