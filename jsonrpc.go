package sqidsd

import "encoding/json"

// JSON-RPC 2.0 error codes.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeServerError    = -32000
)

// Request represents a JSON-RPC 2.0 request object.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	ID      json.RawMessage `json:"id"`
}

// IsNotification reports whether the request has no id field.
// Note that an explicit "id": null is not a notification.
func (r *Request) IsNotification() bool {
	return r.ID == nil
}

// Response represents a JSON-RPC 2.0 response object.
// Result holds a pre-marshaled JSON value so that valid empty results
// such as "" or [] are not dropped by omitempty.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
	ID      json.RawMessage `json:"id"`
}

// RPCError represents a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

var nullID = json.RawMessage("null")

func newResponse(id, result json.RawMessage) *Response {
	if id == nil {
		id = nullID
	}
	return &Response{JSONRPC: "2.0", Result: result, ID: id}
}

func newErrorResponse(id json.RawMessage, code int, message string, data any) *Response {
	if id == nil {
		id = nullID
	}
	return &Response{
		JSONRPC: "2.0",
		Error:   &RPCError{Code: code, Message: message, Data: data},
		ID:      id,
	}
}
