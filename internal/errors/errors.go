package errors

import (
	"context"
	"errors"
	"fmt"
)

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
	cause   error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func New(code Code, msg string) *Error { return &Error{Code: code, Message: msg} }
func Wrap(code Code, msg string, detail any) *Error {
	return &Error{Code: code, Message: msg, Detail: detail}
}

func WrapCause(code Code, msg string, detail any, cause error) *Error {
	return &Error{Code: code, Message: msg, Detail: detail, cause: cause}
}

func FromContext(err error) (*Error, bool) {
	if errors.Is(err, context.DeadlineExceeded) {
		return WrapCause(CodeUnavailable, "tool call context deadline exceeded", map[string]any{
			"cause": "context_deadline_exceeded",
		}, err), true
	}
	if errors.Is(err, context.Canceled) {
		return WrapCause(CodeUnavailable, "tool call context canceled", map[string]any{
			"cause": "context_canceled",
		}, err), true
	}
	return nil, false
}
