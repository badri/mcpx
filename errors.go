package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// Error codes for structured error responses
const (
	ErrDaemonNotRunning = "DAEMON_NOT_RUNNING"
	ErrConnectionFailed = "CONNECTION_FAILED"
	ErrTimeout          = "TIMEOUT"
	ErrAuthExpired      = "AUTH_EXPIRED"
	ErrUnknownTool      = "UNKNOWN_TOOL"
	ErrInvalidArgs      = "INVALID_ARGS"
	ErrSchemaError      = "SCHEMA_ERROR"
	ErrMCPError         = "MCP_ERROR"
	ErrParseError       = "PARSE_ERROR"
	ErrNotFound         = "NOT_FOUND"
	ErrExists           = "EXISTS"
	ErrMissingDep       = "MISSING_DEP"
	ErrInvalidJSON      = "INVALID_JSON"
	ErrDaemonError      = "DAEMON_ERROR"
	ErrUnknownAction    = "UNKNOWN_ACTION"
)

// ErrorResponse represents a structured error
type ErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Response is the standard response format
type Response struct {
	OK    bool           `json:"ok"`
	Data  any            `json:"data,omitempty"`
	Error *ErrorResponse `json:"error,omitempty"`
}

// ok prints a success response and exits
func ok(data any) {
	resp := Response{OK: true, Data: data}
	out, _ := json.MarshalIndent(resp, "", "  ")
	fmt.Println(string(out))
	os.Exit(0)
}

// errExit prints an error response and exits
func errExit(code, message string) {
	resp := Response{
		OK:    false,
		Error: &ErrorResponse{Code: code, Message: message},
	}
	out, _ := json.MarshalIndent(resp, "", "  ")
	fmt.Println(string(out))
	os.Exit(1)
}

// errResponse returns an error response (for daemon use, no exit)
func errResponse(code, message string) Response {
	return Response{
		OK:    false,
		Error: &ErrorResponse{Code: code, Message: message},
	}
}

// okResponse returns a success response (for daemon use, no exit)
func okResponse(data any) Response {
	return Response{OK: true, Data: data}
}
