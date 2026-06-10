package sqidsd

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func newTestServer(t *testing.T, opts *Options) *Server {
	t.Helper()
	if opts == nil {
		opts = &Options{}
	}
	if len(opts.Addresses) == 0 {
		opts.Addresses = []string{"127.0.0.1:0"}
	}
	s, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func marshalResponse(t *testing.T, resp *Response) string {
	t.Helper()
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestHandleLine(t *testing.T) {
	s := newTestServer(t, nil)
	tests := []struct {
		name string
		line string
		want string
	}{
		{
			"encode",
			`{"jsonrpc":"2.0","id":1,"method":"encode","params":[1,2,3]}`,
			`{"jsonrpc":"2.0","result":"86Rf07","id":1}`,
		},
		{
			"decode",
			`{"jsonrpc":"2.0","id":2,"method":"decode","params":["86Rf07"]}`,
			`{"jsonrpc":"2.0","result":[1,2,3],"id":2}`,
		},
		{
			"encode empty array",
			`{"jsonrpc":"2.0","id":3,"method":"encode","params":[]}`,
			`{"jsonrpc":"2.0","result":"","id":3}`,
		},
		{
			"decode empty string",
			`{"jsonrpc":"2.0","id":4,"method":"decode","params":[""]}`,
			`{"jsonrpc":"2.0","result":[],"id":4}`,
		},
		{
			"string id is echoed",
			`{"jsonrpc":"2.0","id":"abc","method":"encode","params":[1,2,3]}`,
			`{"jsonrpc":"2.0","result":"86Rf07","id":"abc"}`,
		},
		{
			"large numeric id is echoed exactly",
			`{"jsonrpc":"2.0","id":12345678901234567890,"method":"encode","params":[1,2,3]}`,
			`{"jsonrpc":"2.0","result":"86Rf07","id":12345678901234567890}`,
		},
		{
			"explicit null id gets a response",
			`{"jsonrpc":"2.0","id":null,"method":"encode","params":[1,2,3]}`,
			`{"jsonrpc":"2.0","result":"86Rf07","id":null}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := s.handleLine([]byte(tt.line))
			if resp == nil {
				t.Fatal("expected a response, got nil")
			}
			if got := marshalResponse(t, resp); got != tt.want {
				t.Errorf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestHandleLineErrors(t *testing.T) {
	s := newTestServer(t, nil)
	tests := []struct {
		name     string
		line     string
		wantCode int
		wantID   string
	}{
		{"parse error", `{invalid json`, CodeParseError, "null"},
		{"batch request", `[{"jsonrpc":"2.0","id":1,"method":"encode","params":[1]}]`, CodeInvalidRequest, "null"},
		{"missing jsonrpc", `{"id":1,"method":"encode","params":[1]}`, CodeInvalidRequest, "1"},
		{"wrong jsonrpc version", `{"jsonrpc":"1.0","id":1,"method":"encode","params":[1]}`, CodeInvalidRequest, "1"},
		{"missing method", `{"jsonrpc":"2.0","id":1}`, CodeInvalidRequest, "1"},
		{"method not found", `{"jsonrpc":"2.0","id":1,"method":"foo"}`, CodeMethodNotFound, "1"},
		{"encode without params", `{"jsonrpc":"2.0","id":1,"method":"encode"}`, CodeInvalidParams, "1"},
		{"encode negative number", `{"jsonrpc":"2.0","id":1,"method":"encode","params":[-1]}`, CodeInvalidParams, "1"},
		{"encode float", `{"jsonrpc":"2.0","id":1,"method":"encode","params":[1.5]}`, CodeInvalidParams, "1"},
		{"encode uint64 overflow", `{"jsonrpc":"2.0","id":1,"method":"encode","params":[18446744073709551616]}`, CodeInvalidParams, "1"},
		{"encode string element", `{"jsonrpc":"2.0","id":1,"method":"encode","params":["a"]}`, CodeInvalidParams, "1"},
		{"encode object params", `{"jsonrpc":"2.0","id":1,"method":"encode","params":{"numbers":[1]}}`, CodeInvalidParams, "1"},
		{"decode empty params", `{"jsonrpc":"2.0","id":1,"method":"decode","params":[]}`, CodeInvalidParams, "1"},
		{"decode two strings", `{"jsonrpc":"2.0","id":1,"method":"decode","params":["a","b"]}`, CodeInvalidParams, "1"},
		{"decode number param", `{"jsonrpc":"2.0","id":1,"method":"decode","params":[1]}`, CodeInvalidParams, "1"},
		{"decode invalid sqids string", `{"jsonrpc":"2.0","id":1,"method":"decode","params":["!!!"]}`, CodeServerError, "1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := s.handleLine([]byte(tt.line))
			if resp == nil {
				t.Fatal("expected a response, got nil")
			}
			if resp.Error == nil {
				t.Fatalf("expected an error response, got %s", marshalResponse(t, resp))
			}
			if resp.Error.Code != tt.wantCode {
				t.Errorf("got code %d, want %d", resp.Error.Code, tt.wantCode)
			}
			if string(resp.ID) != tt.wantID {
				t.Errorf("got id %s, want %s", string(resp.ID), tt.wantID)
			}
			if resp.Result != nil {
				t.Errorf("error response must not have a result: %s", marshalResponse(t, resp))
			}
		})
	}
}

func TestHandleLineNotification(t *testing.T) {
	s := newTestServer(t, nil)
	lines := []string{
		`{"jsonrpc":"2.0","method":"encode","params":[1,2,3]}`,
		`{"jsonrpc":"2.0","method":"foo"}`,
		`{"jsonrpc":"2.0","method":"encode","params":["invalid"]}`,
	}
	for _, line := range lines {
		if resp := s.handleLine([]byte(line)); resp != nil {
			t.Errorf("notification %s must not get a response, got %s", line, marshalResponse(t, resp))
		}
	}
}

func TestHandleLineUint64Max(t *testing.T) {
	s := newTestServer(t, nil)
	const max = "18446744073709551615"
	resp := s.handleLine([]byte(`{"jsonrpc":"2.0","id":1,"method":"encode","params":[` + max + `]}`))
	if resp == nil || resp.Error != nil {
		t.Fatalf("unexpected response: %s", marshalResponse(t, resp))
	}
	var id string
	if err := json.Unmarshal(resp.Result, &id); err != nil {
		t.Fatal(err)
	}
	resp = s.handleLine(fmt.Appendf(nil, `{"jsonrpc":"2.0","id":2,"method":"decode","params":[%q]}`, id))
	if resp == nil || resp.Error != nil {
		t.Fatalf("unexpected response: %s", marshalResponse(t, resp))
	}
	if got := string(resp.Result); got != "["+max+"]" {
		t.Errorf("got %s, want [%s]", got, max)
	}
}

func TestHandleLineCustomOptions(t *testing.T) {
	s := newTestServer(t, &Options{
		Addresses: []string{"127.0.0.1:0"},
		Alphabet:  "abcdefghij",
		MinLength: 10,
	})
	resp := s.handleLine([]byte(`{"jsonrpc":"2.0","id":1,"method":"encode","params":[1,2,3]}`))
	if resp == nil || resp.Error != nil {
		t.Fatalf("unexpected response: %s", marshalResponse(t, resp))
	}
	var id string
	if err := json.Unmarshal(resp.Result, &id); err != nil {
		t.Fatal(err)
	}
	if len(id) < 10 {
		t.Errorf("id %q is shorter than min length 10", id)
	}
	if strings.Trim(id, "abcdefghij") != "" {
		t.Errorf("id %q contains characters outside the custom alphabet", id)
	}
	resp = s.handleLine(fmt.Appendf(nil, `{"jsonrpc":"2.0","id":2,"method":"decode","params":[%q]}`, id))
	if got := string(resp.Result); got != "[1,2,3]" {
		t.Errorf("got %s, want [1,2,3]", got)
	}
}

func TestNewWithoutListeners(t *testing.T) {
	if _, err := New(&Options{}); err == nil {
		t.Error("New must fail when no listener address is given")
	}
}

func TestNewWithInvalidAlphabet(t *testing.T) {
	if _, err := New(&Options{Addresses: []string{"127.0.0.1:0"}, Alphabet: "ab"}); err == nil {
		t.Error("New must fail with a too short alphabet")
	}
}
