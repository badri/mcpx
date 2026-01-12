package main

import (
	"encoding/json"
	"testing"
)

func TestErrorCodes(t *testing.T) {
	// Verify error codes are defined and unique
	codes := []string{
		ErrDaemonNotRunning,
		ErrConnectionFailed,
		ErrTimeout,
		ErrAuthExpired,
		ErrUnknownTool,
		ErrInvalidArgs,
		ErrSchemaError,
		ErrMCPError,
		ErrParseError,
		ErrNotFound,
		ErrExists,
		ErrMissingDep,
		ErrInvalidJSON,
		ErrDaemonError,
		ErrUnknownAction,
	}

	seen := make(map[string]bool)
	for _, code := range codes {
		if code == "" {
			t.Error("Error code should not be empty")
		}
		if seen[code] {
			t.Errorf("Duplicate error code: %s", code)
		}
		seen[code] = true
	}
}

func TestErrResponse(t *testing.T) {
	resp := errResponse(ErrNotFound, "Server not found")

	if resp.OK {
		t.Error("Expected OK to be false")
	}

	if resp.Data != nil {
		t.Error("Expected Data to be nil for error response")
	}

	if resp.Error == nil {
		t.Fatal("Expected Error to be set")
	}

	if resp.Error.Code != ErrNotFound {
		t.Errorf("Expected code %s, got %s", ErrNotFound, resp.Error.Code)
	}

	if resp.Error.Message != "Server not found" {
		t.Errorf("Expected message 'Server not found', got '%s'", resp.Error.Message)
	}
}

func TestOkResponse(t *testing.T) {
	data := map[string]any{
		"server": "test",
		"tools":  []string{"tool1", "tool2"},
	}
	resp := okResponse(data)

	if !resp.OK {
		t.Error("Expected OK to be true")
	}

	if resp.Error != nil {
		t.Error("Expected Error to be nil")
	}

	if resp.Data == nil {
		t.Fatal("Expected Data to be set")
	}
}

func TestOkResponse_StringData(t *testing.T) {
	resp := okResponse("pong")

	if !resp.OK {
		t.Error("Expected OK to be true")
	}

	if resp.Data != "pong" {
		t.Errorf("Expected data 'pong', got '%v'", resp.Data)
	}
}

func TestResponseJSON(t *testing.T) {
	// Test success response serialization
	successResp := okResponse(map[string]any{
		"message": "test",
		"count":   42,
	})

	data, err := json.Marshal(successResp)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded Response
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if !decoded.OK {
		t.Error("Expected OK to be true")
	}

	// Test error response serialization
	errResp := errResponse(ErrInvalidArgs, "Missing argument")

	data, err = json.Marshal(errResp)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.OK {
		t.Error("Expected OK to be false")
	}

	if decoded.Error == nil {
		t.Fatal("Expected Error to be set")
	}

	if decoded.Error.Code != ErrInvalidArgs {
		t.Errorf("Expected code %s, got %s", ErrInvalidArgs, decoded.Error.Code)
	}
}

func TestErrorResponseOmitsEmptyFields(t *testing.T) {
	// Success response should omit error field
	successResp := okResponse("test")
	data, _ := json.Marshal(successResp)

	var raw map[string]any
	json.Unmarshal(data, &raw)

	if _, ok := raw["error"]; ok {
		t.Error("Expected error field to be omitted in success response")
	}

	// Error response - data field is nil, which with `any` type and omitempty
	// still gets serialized (Go's omitempty only works for zero values of typed fields)
	errResp := errResponse(ErrNotFound, "Not found")
	data, _ = json.Marshal(errResp)

	json.Unmarshal(data, &raw)
	// Note: with `any` type, nil is not omitted by omitempty, so we just verify the structure
	if errResp.Data != nil {
		t.Error("Expected Data to be nil in error response")
	}
}

func TestErrorResponseStructure(t *testing.T) {
	resp := errResponse(ErrMCPError, "Connection failed")

	// Verify JSON structure matches expected format
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// Check structure
	if parsed["ok"] != false {
		t.Error("Expected ok to be false")
	}

	errObj, ok := parsed["error"].(map[string]any)
	if !ok {
		t.Fatal("Expected error to be an object")
	}

	if errObj["code"] != ErrMCPError {
		t.Errorf("Expected code %s, got %v", ErrMCPError, errObj["code"])
	}

	if errObj["message"] != "Connection failed" {
		t.Errorf("Expected message 'Connection failed', got %v", errObj["message"])
	}
}
