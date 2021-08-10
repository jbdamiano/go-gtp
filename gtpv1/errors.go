// Copyright 2019-2021 go-gtp authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

package gtpv1

import (
	"errors"
	"fmt"
)

var (
	// ErrUnexpectedType indicates that the type of incoming message is not expected.
	ErrUnexpectedType = errors.New("got unexpected type of message")

	// ErrInvalidConnection indicates that the connection type(C-Plane or U-Plane) is
	// not the expected one.
	ErrInvalidConnection = errors.New("got invalid connection type")

	// ErrConnNotOpened indicates that some operation is failed due to the status of
	// Conn is not valid.
	ErrConnNotOpened = errors.New("connection is not opened")

	// ErrTEIDNotFound indicates that TEID is not registered for the interface specified.
	ErrTEIDNotFound = errors.New("no TEID found")

	// ErrTimeout indicates that a handler failed to complete its work due to the
	// absence of message expected to come from another endpoint.
	ErrTimeout = errors.New("timed out")
)

// ErrorIndicatedError indicates that Error Indication message is received on U-Plane Connection.
type ErrorIndicatedError struct {
	TEID uint32
	Peer string
}

func (e *ErrorIndicatedError) Error() string {
	return fmt.Sprintf("error received from %s, TEIDDataI: %#x", e.Peer, e.TEID)
}

// HandlerNotFoundError indicates that the handler func is not registered in *Conn
// for the incoming GTPv2 message. In usual cases this error should not be taken
// as fatal, as the other endpoint can make your program stop working just by
// sending unregistered message.
type HandlerNotFoundError struct {
	MsgType string
}

// Error returns violating message type to handle.
func (e *HandlerNotFoundError) Error() string {
	return fmt.Sprintf("no handlers found for incoming message: %s, ignoring", e.MsgType)
}

// RequiredParameterMissingError indicates that no Bearer found by lookup methods.
type RequiredParameterMissingError struct {
	Name, Msg string
}

// Error returns missing parameter with message.
func (e *RequiredParameterMissingError) Error() string {
	return fmt.Sprintf("required parameter: %s is missing. %s", e.Name, e.Msg)
}

// InvalidSequenceError indicates that the Sequence Number is invalid.
type InvalidSequenceError struct {
	Seq uint16
}

// Error returns violating Sequence Number.
func (e *InvalidSequenceError) Error() string {
	return fmt.Sprintf("got invalid Sequence Number: %d", e.Seq)
}

// InvalidSessionError indicates that something went wrong with Session.
type InvalidSessionError struct {
	IMSI string
}

// Error returns message with IMSI associated with Session if available.
func (e *InvalidSessionError) Error() string {
	return fmt.Sprintf("invalid session, IMSI: %s", e.IMSI)
}

// InvalidTEIDError indicates that the TEID value is different from expected one or
// not registered in TEIDMap.
type InvalidTEIDError struct {
	TEID uint32
}

// Error returns violating TEID.
func (e *InvalidTEIDError) Error() string {
	return fmt.Sprintf("got invalid TEID: %#08x", e.TEID)
}

// UnknownIMSIError indicates that the IMSI is different from expected one.
type UnknownIMSIError struct {
	IMSI string
}

// Error returns violating IMSI.
func (e *UnknownIMSIError) Error() string {
	return fmt.Sprintf("got unknown IMSI: %s", e.IMSI)
}

// CauseNotOKError indicates that the value in Cause IE is not OK.
type CauseNotOKError struct {
	MsgType string
	Cause   uint8
	Msg     string
}

// Error returns error cause with message.
func (e *CauseNotOKError) Error() string {
	return fmt.Sprintf("got non-OK Cause: %d in %s; %s", e.Cause, e.MsgType, e.Msg)
}
