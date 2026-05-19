package jsonrpc

import (
	"encoding/json"
	"testing"
)

func TestID_NewIntID(t *testing.T) {
	id := NewIntID(42)

	if !id.IsInt() {
		t.Error("NewIntID should create int ID")
	}
	if id.IsString() {
		t.Error("Int ID should not be string")
	}
	if id.IsNull() {
		t.Error("Int ID should not be null")
	}
	if id.Int() != 42 {
		t.Errorf("Int() = %d, want 42", id.Int())
	}
}

func TestID_NewStringID(t *testing.T) {
	id := NewStringID("abc-123")

	if !id.IsString() {
		t.Error("NewStringID should create string ID")
	}
	if id.IsInt() {
		t.Error("String ID should not be int")
	}
	if id.IsNull() {
		t.Error("String ID should not be null")
	}
	if id.String() != "abc-123" {
		t.Errorf("String() = %q, want %q", id.String(), "abc-123")
	}
}

func TestID_NullID(t *testing.T) {
	id := NullID()

	if !id.IsNull() {
		t.Error("NullID should create null ID")
	}
	if id.IsInt() {
		t.Error("Null ID should not be int")
	}
	if id.IsString() {
		t.Error("Null ID should not be string")
	}
}

func TestID_ZeroValueIsNull(t *testing.T) {
	var id ID

	if !id.IsNull() {
		t.Error("Zero value ID should be null")
	}
}

func TestID_Equal(t *testing.T) {
	tests := []struct {
		name  string
		id1   ID
		id2   ID
		equal bool
	}{
		{"null == null", NullID(), NullID(), true},
		{"int == int same", NewIntID(1), NewIntID(1), true},
		{"int == int diff", NewIntID(1), NewIntID(2), false},
		{"string == string same", NewStringID("a"), NewStringID("a"), true},
		{"string == string diff", NewStringID("a"), NewStringID("b"), false},
		{"int != string", NewIntID(1), NewStringID("1"), false},
		{"int != null", NewIntID(1), NullID(), false},
		{"string != null", NewStringID("a"), NullID(), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.id1.Equal(tt.id2); got != tt.equal {
				t.Errorf("Equal() = %v, want %v", got, tt.equal)
			}
		})
	}
}

func TestID_MarshalJSON(t *testing.T) {
	tests := []struct {
		name string
		id   ID
		want string
	}{
		{"null", NullID(), "null"},
		{"int", NewIntID(42), "42"},
		{"string", NewStringID("test"), `"test"`},
		{"zero value", ID{}, "null"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.id.MarshalJSON()
			if err != nil {
				t.Fatalf("MarshalJSON() error = %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("MarshalJSON() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestID_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		wantInt bool
		wantStr bool
		wantNul bool
		intVal  int64
		strVal  string
		wantErr bool
	}{
		{"null", "null", false, false, true, 0, "", false},
		{"int", "42", true, false, false, 42, "", false},
		{"string", `"test"`, false, true, false, 0, "test", false},
		{"float becomes int", "3.14", true, false, false, 3, "", false},
		{"invalid", `[1,2,3]`, false, false, false, 0, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var id ID
			err := id.UnmarshalJSON([]byte(tt.json))
			if (err != nil) != tt.wantErr {
				t.Errorf("UnmarshalJSON() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if id.IsInt() != tt.wantInt {
				t.Errorf("IsInt() = %v, want %v", id.IsInt(), tt.wantInt)
			}
			if id.IsString() != tt.wantStr {
				t.Errorf("IsString() = %v, want %v", id.IsString(), tt.wantStr)
			}
			if id.IsNull() != tt.wantNul {
				t.Errorf("IsNull() = %v, want %v", id.IsNull(), tt.wantNul)
			}
			if tt.wantInt && id.Int() != tt.intVal {
				t.Errorf("Int() = %d, want %d", id.Int(), tt.intVal)
			}
			if tt.wantStr && id.String() != tt.strVal {
				t.Errorf("String() = %q, want %q", id.String(), tt.strVal)
			}
		})
	}
}

func TestError_Error(t *testing.T) {
	e := &Error{
		Code:    CodeInvalidRequest,
		Message: "test error",
	}

	if e.Error() != "test error" {
		t.Errorf("Error() = %q, want %q", e.Error(), "test error")
	}
}

func TestNewError(t *testing.T) {
	e := NewError(CodeMethodNotFound, "method not found")

	if e.Code != CodeMethodNotFound {
		t.Errorf("Code = %d, want %d", e.Code, CodeMethodNotFound)
	}
	if e.Message != "method not found" {
		t.Errorf("Message = %q, want %q", e.Message, "method not found")
	}
	if e.Data != nil {
		t.Error("Data should be nil")
	}
}

func TestNewErrorWithData(t *testing.T) {
	data := json.RawMessage(`{"detail":"more info"}`)
	e := NewErrorWithData(CodeInternalError, "internal error", data)

	if e.Code != CodeInternalError {
		t.Errorf("Code = %d, want %d", e.Code, CodeInternalError)
	}
	if e.Message != "internal error" {
		t.Errorf("Message = %q, want %q", e.Message, "internal error")
	}
	if string(e.Data) != `{"detail":"more info"}` {
		t.Errorf("Data = %s, want %s", e.Data, data)
	}
}

func TestNewRequest(t *testing.T) {
	tests := []struct {
		name    string
		id      ID
		method  string
		params  any
		wantErr bool
	}{
		{
			name:   "with params",
			id:     NewIntID(1),
			method: "test.method",
			params: map[string]string{"key": "value"},
		},
		{
			name:   "nil params",
			id:     NewStringID("abc"),
			method: "test.method",
			params: nil,
		},
		{
			name:   "null id",
			id:     NullID(),
			method: "notification",
			params: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := NewRequest(tt.id, tt.method, tt.params)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewRequest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if req == nil {
				return
			}
			if req.JSONRPC != "2.0" {
				t.Errorf("JSONRPC = %q, want %q", req.JSONRPC, "2.0")
			}
			if !req.ID.Equal(tt.id) {
				t.Error("ID mismatch")
			}
			if req.Method != tt.method {
				t.Errorf("Method = %q, want %q", req.Method, tt.method)
			}
		})
	}
}

func TestNewRequest_Serialization(t *testing.T) {
	req, err := NewRequest(NewIntID(1), "test", map[string]int{"x": 42})
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var parsed Request
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if parsed.JSONRPC != "2.0" {
		t.Errorf("JSONRPC = %q, want %q", parsed.JSONRPC, "2.0")
	}
	if !parsed.ID.IsInt() || parsed.ID.Int() != 1 {
		t.Error("ID mismatch after round-trip")
	}
	if parsed.Method != "test" {
		t.Errorf("Method = %q, want %q", parsed.Method, "test")
	}
}

func TestNewResponse(t *testing.T) {
	tests := []struct {
		name    string
		id      ID
		result  any
		wantErr bool
	}{
		{
			name:   "with result",
			id:     NewIntID(1),
			result: map[string]bool{"success": true},
		},
		{
			name:   "nil result",
			id:     NewIntID(2),
			result: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := NewResponse(tt.id, tt.result)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewResponse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if resp == nil {
				return
			}
			if resp.JSONRPC != "2.0" {
				t.Errorf("JSONRPC = %q, want %q", resp.JSONRPC, "2.0")
			}
			if !resp.ID.Equal(tt.id) {
				t.Error("ID mismatch")
			}
			if resp.Error != nil {
				t.Error("Error should be nil for success response")
			}
		})
	}
}

func TestNewErrorResponse(t *testing.T) {
	resp := NewErrorResponse(NewIntID(1), CodeInvalidParams, "invalid params")

	if resp.JSONRPC != "2.0" {
		t.Errorf("JSONRPC = %q, want %q", resp.JSONRPC, "2.0")
	}
	if !resp.ID.Equal(NewIntID(1)) {
		t.Error("ID mismatch")
	}
	if resp.Result != nil {
		t.Error("Result should be nil for error response")
	}
	if resp.Error == nil {
		t.Fatal("Error should not be nil")
	}
	if resp.Error.Code != CodeInvalidParams {
		t.Errorf("Error.Code = %d, want %d", resp.Error.Code, CodeInvalidParams)
	}
	if resp.Error.Message != "invalid params" {
		t.Errorf("Error.Message = %q, want %q", resp.Error.Message, "invalid params")
	}
}

func TestResponse_Serialization(t *testing.T) {
	// Success response
	successResp, _ := NewResponse(NewIntID(1), map[string]string{"data": "test"})
	successData, _ := json.Marshal(successResp)

	var parsedSuccess Response
	if err := json.Unmarshal(successData, &parsedSuccess); err != nil {
		t.Fatalf("Unmarshal success() error = %v", err)
	}
	if parsedSuccess.Error != nil {
		t.Error("Success response should not have error")
	}

	// Error response
	errorResp := NewErrorResponse(NewIntID(2), CodeMethodNotFound, "not found")
	errorData, _ := json.Marshal(errorResp)

	var parsedError Response
	if err := json.Unmarshal(errorData, &parsedError); err != nil {
		t.Fatalf("Unmarshal error() error = %v", err)
	}
	if parsedError.Error == nil {
		t.Fatal("Error response should have error")
	}
	if parsedError.Error.Code != CodeMethodNotFound {
		t.Errorf("Error.Code = %d, want %d", parsedError.Error.Code, CodeMethodNotFound)
	}
}

func TestErrorCodes(t *testing.T) {
	// Standard JSON-RPC codes
	standardCodes := map[int]string{
		CodeParseError:     "CodeParseError",
		CodeInvalidRequest: "CodeInvalidRequest",
		CodeMethodNotFound: "CodeMethodNotFound",
		CodeInvalidParams:  "CodeInvalidParams",
		CodeInternalError:  "CodeInternalError",
	}

	for code, name := range standardCodes {
		if code >= -32599 || code <= -32700 {
			// Standard codes should be in range -32700 to -32600
			if code > -32600 || code < -32700 {
				if code != CodeInternalError { // -32603 is valid
					t.Errorf("%s (%d) outside standard range", name, code)
				}
			}
		}
	}

	// MCP-Meta custom codes
	customCodes := map[int]string{
		CodePodError:         "CodePodError",
		CodePodTimeout:       "CodePodTimeout",
		CodePodCrashed:       "CodePodCrashed",
		CodePodSpawnFailed:   "CodePodSpawnFailed",
		CodeCircularCall:     "CodeCircularCall",
		CodeContractMismatch: "CodeContractMismatch",
	}

	for code, name := range customCodes {
		// Custom codes should be in range -32000 to -32099
		if code < -32099 || code > -32000 {
			t.Errorf("%s (%d) outside custom code range", name, code)
		}
	}
}

func TestContractExpectation(t *testing.T) {
	ce := ContractExpectation{
		ID:         "test-contract",
		Version:    "1.0",
		SchemaHash: "sha256:abc123",
	}

	data, err := json.Marshal(ce)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var parsed ContractExpectation
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if parsed.ID != ce.ID {
		t.Errorf("ID = %q, want %q", parsed.ID, ce.ID)
	}
	if parsed.Version != ce.Version {
		t.Errorf("Version = %q, want %q", parsed.Version, ce.Version)
	}
	if parsed.SchemaHash != ce.SchemaHash {
		t.Errorf("SchemaHash = %q, want %q", parsed.SchemaHash, ce.SchemaHash)
	}
}

func TestRequest_ParamsOmitEmpty(t *testing.T) {
	req, _ := NewRequest(NewIntID(1), "test", nil)
	data, _ := json.Marshal(req)

	// Params should be omitted when nil
	var m map[string]any
	json.Unmarshal(data, &m)

	if _, ok := m["params"]; ok {
		t.Error("params should be omitted when nil")
	}
}
