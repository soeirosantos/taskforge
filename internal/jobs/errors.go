package jobs

import "encoding/json"

// Job-level error codes. These are the codes persisted in a job's "error"
// field (SPEC 9, 34, 35). They are a separate namespace from the HTTP API error
// codes of SPEC 30 and must not be mixed with them.
const (
	CodeIntentionalFailure   = "INTENTIONAL_FAILURE"
	CodeInterruptedExecution = "INTERRUPTED_EXECUTION"
	CodeServerShutdown       = "SERVER_SHUTDOWN"
)

// The exact messages the specification pairs with each job error code.
const (
	MsgIntentionalFailure   = "job failed intentionally"
	MsgInterruptedExecution = "job execution was interrupted by server termination"
	MsgServerShutdown       = "job execution was interrupted by server shutdown"
)

// JobError is the persisted shape of a job's "error" field.
type JobError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Error implements the error interface so an executor can return a JobError
// directly as its failure.
func (e *JobError) Error() string {
	return e.Code + ": " + e.Message
}

// JSON encodes the error for persistence. The struct has only string fields, so
// encoding cannot fail.
func (e *JobError) JSON() json.RawMessage {
	raw, err := json.Marshal(e)
	if err != nil {
		panic("jobs: marshalling JobError: " + err.Error())
	}
	return raw
}

// IntentionalFailure is the error produced by every "fail" job (SPEC 9).
func IntentionalFailure() *JobError {
	return &JobError{Code: CodeIntentionalFailure, Message: MsgIntentionalFailure}
}

// InterruptedExecution is recorded by startup recovery for jobs left RUNNING by
// a terminated process (SPEC 34).
func InterruptedExecution() *JobError {
	return &JobError{Code: CodeInterruptedExecution, Message: MsgInterruptedExecution}
}

// ServerShutdown is recorded for jobs interrupted by graceful shutdown
// (SPEC 35).
func ServerShutdown() *JobError {
	return &JobError{Code: CodeServerShutdown, Message: MsgServerShutdown}
}
