package sqidsd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
)

// handleLine processes a single JSON-RPC 2.0 request line and returns the
// response, or nil when no response should be sent (notification).
func (s *Server) handleLine(line []byte) *Response {
	line = bytes.TrimSpace(line)
	if len(line) > 0 && line[0] == '[' {
		return newErrorResponse(nil, CodeInvalidRequest, "Invalid Request", "batch requests are not supported")
	}
	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		return newErrorResponse(nil, CodeParseError, "Parse error", err.Error())
	}
	if req.JSONRPC != "2.0" || req.Method == "" {
		return newErrorResponse(req.ID, CodeInvalidRequest, "Invalid Request", nil)
	}
	var resp *Response
	switch req.Method {
	case "encode":
		resp = s.encode(&req)
	case "decode":
		resp = s.decode(&req)
	default:
		resp = newErrorResponse(req.ID, CodeMethodNotFound, "Method not found", req.Method)
	}
	if req.IsNotification() {
		return nil
	}
	return resp
}

// encode handles the "encode" method: params is an array of non-negative
// integers, the result is a Sqids ID string.
func (s *Server) encode(req *Request) *Response {
	var nums []json.Number
	if err := json.Unmarshal(req.Params, &nums); err != nil {
		return newErrorResponse(req.ID, CodeInvalidParams, "Invalid params", "params must be an array of non-negative integers")
	}
	numbers := make([]uint64, 0, len(nums))
	for _, n := range nums {
		u, err := strconv.ParseUint(n.String(), 10, 64)
		if err != nil {
			return newErrorResponse(req.ID, CodeInvalidParams, "Invalid params",
				fmt.Sprintf("%q is not a non-negative integer in uint64 range", n.String()))
		}
		numbers = append(numbers, u)
	}
	id, err := s.sqids.Encode(numbers)
	if err != nil {
		return newErrorResponse(req.ID, CodeServerError, "Server error", err.Error())
	}
	result, err := json.Marshal(id)
	if err != nil {
		return newErrorResponse(req.ID, CodeServerError, "Server error", err.Error())
	}
	return newResponse(req.ID, result)
}

// decode handles the "decode" method: params is an array containing one
// Sqids ID string, the result is an array of non-negative integers.
func (s *Server) decode(req *Request) *Response {
	var strs []string
	if err := json.Unmarshal(req.Params, &strs); err != nil || len(strs) != 1 {
		return newErrorResponse(req.ID, CodeInvalidParams, "Invalid params", "params must be an array containing one string")
	}
	id := strs[0]
	numbers := s.sqids.Decode(id)
	if id != "" && len(numbers) == 0 {
		return newErrorResponse(req.ID, CodeServerError, "Server error", "invalid sqids string")
	}
	if numbers == nil {
		numbers = []uint64{}
	}
	result, err := json.Marshal(numbers)
	if err != nil {
		return newErrorResponse(req.ID, CodeServerError, "Server error", err.Error())
	}
	return newResponse(req.ID, result)
}
