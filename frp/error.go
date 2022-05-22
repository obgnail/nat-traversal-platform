package frp

import (
	"fmt"
)

// string
const (
	ModelMessage  = "ErrArgMessage"
	ModelConn     = "ErrArgConn"
	ModelApp      = "ErrArgApp"
	ModelClient   = "ErrArgClient"
	ModelServer   = "ErrArgServer"
	ModelListener = "ErrArgListener"
)

// ErrReason
const (
	ReasonNotFound  = "NotFound"
	ReasonUnmarshal = "Unmarshal"
	ReasonMarshal   = "Marshal"
	ReasonEmpty     = "Empty"
	ReasonClose     = "Close"
	ReasonSend      = "Send"
	ReasonJoin      = "Join"
	ReasonPassword  = "Password"
)

// ErrArg
const (
	ErrArgApp    = "App"
	ErrArgServer = "Server"
	ErrArgClient = "Client"

	ErrArgListener = "Listener"
	ErrArgConn     = "Conn"

	ErrArgMeta      = "Meta"
	ErrArgType      = "type"
	ErrArgMessage   = "Message"
	ErrArgHeartbeat = "Heartbeat"
)

func new(errModel string, errReason string, args interface{}) error {
	return fmt.Errorf("[%s.%s] [%s]", errModel, errReason, args)
}

func NotFoundError(errModel string, args interface{}) error {
	return new(errModel, ReasonNotFound, args)
}

func EmptyError(errModel string, args interface{}) error {
	return new(errModel, ReasonEmpty, args)
}

func ConnCloseError() error {
	return new(ModelConn, ReasonClose, "")
}

func SendMessageError(args interface{}) error {
	return new(ModelMessage, ReasonSend, args)
}

func UnmarshalMessageError(args interface{}) error {
	return new(ModelMessage, ReasonUnmarshal, args)
}

func MarshalMessageError(args interface{}) error {
	return new(ModelMessage, ReasonMarshal, args)
}

func JoinConnError() error {
	return new(ModelConn, ReasonJoin, "")
}

func SendHeartbeatMessageError() error {
	return new(ModelMessage, ReasonSend, ErrArgHeartbeat)
}

func InvalidPasswordError(args interface{}) error {
	return new(ModelApp, ReasonPassword, args)
}
