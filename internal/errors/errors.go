package errors

import "fmt"

type Code string

const (
	CodeInvalidRequest   Code = "invalid_request"
	CodeNotFound         Code = "not_found"
	CodePermissionDenied Code = "permission_denied"
	CodeUnavailable      Code = "unavailable"
	CodeUnsupported      Code = "unsupported"
	CodeMutationDisabled Code = "mutation_disabled"
	CodeJenkins          Code = "jenkins_error"
)

type Error struct {
	Code    Code   `json:"code"`
	Message string `json:"message"`
	Detail  any    `json:"detail,omitempty"`
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}
func New(code Code, msg string) *Error { return &Error{Code: code, Message: msg} }
func Wrap(code Code, msg string, detail any) *Error {
	return &Error{Code: code, Message: msg, Detail: detail}
}
