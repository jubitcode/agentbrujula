package jsonrpc

import (
	"encoding/json"
	"fmt"
)

// Error codes - JSON-RPC 2.0 standard codes
const (
	CodeParseError     = -32700 // Invalid JSON
	CodeInvalidRequest = -32600 // Not a valid Request object
	CodeMethodNotFound = -32601 // Method not found
	CodeInvalidParams  = -32602 // Invalid method parameters
	CodeInternalError  = -32603 // Internal JSON-RPC error
)

// Error codes - MCP-Meta custom codes
const (
	CodePodError         = -32000 // Pod returned an error
	CodePodTimeout       = -32001 // Pod call exceeded timeout
	CodePodCrashed       = -32002 // Pod process died
	CodePodSpawnFailed   = -32003 // Could not start pod
	CodeCircularCall     = -32004 // Inter-tool call cycle detected
	CodeContractMismatch = -32005 // Tool contract verification failed
	CodeCircuitOpen      = -32006 // Circuit breaker is open
)

// ID Per JSON-RPC 2.0 spec, ID can be String, Number (integer), or Null.
type ID struct {
	intVal    int64
	stringVal string
	isNull    bool
	isInt     bool
	isString  bool
}

func NewIntID(n int64) ID {
	return ID{intVal: n, isInt: true}
}

func NewStringID(s string) ID {
	return ID{stringVal: s, isString: true}
}

func NullID() ID {
	return ID{isNull: true}
}

func (id ID) IsInt() bool {
	return id.isInt
}

func (id ID) IsString() bool {
	return id.isString
}

// IsNull When a JSON-RPC request omits the id field, it's treated as a notification (null ID).
func (id ID) IsNull() bool {
	return id.isNull || (!id.isInt && !id.isString)
}

func (id ID) Int() int64 {
	return id.intVal
}

func (id ID) String() string {
	return id.stringVal
}

func (id ID) Equal(other ID) bool {
	if id.IsNull() && other.IsNull() {
		return true
	}
	if id.isInt && other.isInt {
		return id.intVal == other.intVal
	}
	if id.isString && other.isString {
		return id.stringVal == other.stringVal
	}
	return false
}

func (id ID) MarshalJSON() ([]byte, error) {
	if id.isNull {
		return []byte("null"), nil
	}
	if id.isInt {
		return json.Marshal(id.intVal)
	}
	if id.isString {
		return json.Marshal(id.stringVal)
	}
	return []byte("null"), nil
}

func (id *ID) UnmarshalJSON(data []byte) error {
	*id = ID{}

	if string(data) == "null" {
		id.isNull = true
		return nil
	}

	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		id.stringVal = s
		id.isString = true
		return nil
	}

	var n float64
	if err := json.Unmarshal(data, &n); err == nil {
		id.intVal = int64(n)
		id.isInt = true
		return nil
	}

	return fmt.Errorf("invalid ID type: %s", string(data))
}

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      ID              `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      ID              `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

type Error struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *Error) Error() string {
	return e.Message
}

func NewError(code int, message string) *Error {
	return &Error{
		Code:    code,
		Message: message,
	}
}

func NewErrorWithData(code int, message string, data json.RawMessage) *Error {
	return &Error{
		Code:    code,
		Message: message,
		Data:    data,
	}
}

func NewRequest(id ID, method string, params any) (*Request, error) {
	var paramsJSON json.RawMessage
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshal params: %w", err)
		}
		paramsJSON = data
	}

	return &Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  paramsJSON,
	}, nil
}

func NewResponse(id ID, result any) (*Response, error) {
	var resultJSON json.RawMessage
	if result != nil {
		data, err := json.Marshal(result)
		if err != nil {
			return nil, fmt.Errorf("marshal result: %w", err)
		}
		resultJSON = data
	}

	return &Response{
		JSONRPC: "2.0",
		ID:      id,
		Result:  resultJSON,
	}, nil
}

func NewErrorResponse(id ID, code int, message string) *Response {
	return &Response{
		JSONRPC: "2.0",
		ID:      id,
		Error:   NewError(code, message),
	}
}

type ContractExpectation struct {
	ID         string `json:"id,omitempty"`
	Version    string `json:"version,omitempty"`
	SchemaHash string `json:"schema_hash,omitempty"`
}
