package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// errorBody is the shape of every error response:
// {"error": {"code": "...", "message": "...", "fields": {...}}}
type errorBody struct {
	Error struct {
		Code    string            `json:"code"`
		Message string            `json:"message"`
		Fields  map[string]string `json:"fields,omitempty"`
	} `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string, fields map[string]string) {
	var b errorBody
	b.Error.Code = code
	b.Error.Message = msg
	b.Error.Fields = fields
	writeJSON(w, status, b)
}

const maxBodyBytes = 1 << 20 // 1 MB

// decodeJSON reads a JSON body into dst, rejecting unknown fields and
// trailing data. It writes the error response itself and returns false
// when the body is unusable.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	return decodeJSONMax(w, r, dst, maxBodyBytes)
}

// decodeJSONMax is decodeJSON with a different size limit (sync pushes).
func decodeJSONMax(w http.ResponseWriter, r *http.Request, dst any, max int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, max)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "body_too_large", "Request body is too large.", nil)
		} else {
			writeError(w, http.StatusBadRequest, "bad_json", "Request body is not valid JSON for this endpoint.", nil)
		}
		return false
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, "bad_json", "Request body must be a single JSON object.", nil)
		return false
	}
	return true
}
