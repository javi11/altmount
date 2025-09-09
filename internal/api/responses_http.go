package api

import (
	"encoding/json"
	"net/http"
)

// HTTP response helpers for legacy handlers

// WriteSuccess writes a success response
func WriteSuccess(w http.ResponseWriter, data interface{}, meta interface{}) {
	response := map[string]interface{}{
		"success": true,
		"data":    data,
	}
	if meta != nil {
		response["meta"] = meta
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// WriteInternalError writes an internal server error response
func WriteInternalError(w http.ResponseWriter, message string, details string) {
	response := map[string]interface{}{
		"success": false,
		"message": message,
	}
	if details != "" {
		response["details"] = details
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	json.NewEncoder(w).Encode(response)
}

// WriteUnauthorized writes an unauthorized response
func WriteUnauthorized(w http.ResponseWriter, message string, details string) {
	response := map[string]interface{}{
		"success": false,
		"message": message,
	}
	if details != "" {
		response["details"] = details
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(response)
}

// WriteForbidden writes a forbidden response
func WriteForbidden(w http.ResponseWriter, message string, details string) {
	response := map[string]interface{}{
		"success": false,
		"message": message,
	}
	if details != "" {
		response["details"] = details
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	json.NewEncoder(w).Encode(response)
}

// WriteBadRequest writes a bad request response
func WriteBadRequest(w http.ResponseWriter, message string, details string) {
	response := map[string]interface{}{
		"success": false,
		"message": message,
	}
	if details != "" {
		response["details"] = details
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(response)
}
