package log_v1

import (
	"fmt"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/status"
)

// ErrOffsetOutOfRange is a custom error that the server will send back to the
// client when the client tries to consume an offset that's outside of the log.
type ErrOffsetOutOfRange struct {
	Offset uint64
}

func (e ErrOffsetOutOfRange) GRPCStatus() *status.Status {
	// Use the grpc status package to build errors with status codes or whatever
	// other data you want to include in your errors.

	// First create a new offset out of range status.
	st := status.New(
		404,
		fmt.Sprintf("offset out of range: %d", e.Offset),
	)

	// The `errdetails` package provides some protobufs you'll likely find useful
	// when building your service, including messages to use to handle bad
	// requests, debug info, and localized messages.

	// Let's use the `LocalizedMessage` from the `errdetails` package to respond
	// with an error message that's safe to return to the user.
	msg := fmt.Sprintf("The requested offset is outside the log's range: %d",
		e.Offset)
	d := &errdetails.LocalizedMessage{
		Locale:  "en-US",
		Message: msg,
	}
	// Next attach the details to the status.
	std, err := st.WithDetails(d)
	if err != nil {
		return st
	}
	return std
}

func (e ErrOffsetOutOfRange) Error() string {
	// Convert and return the status as a Go error.
	return e.GRPCStatus().Err().Error()
}
