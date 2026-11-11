package respond

import (
	"encoding/json"
	"net/http"

	"sports-analytics/gateway/model"
)

func JSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func Error(w http.ResponseWriter, status int, code string, message string) {
	JSON(w, status, model.Error{Error: model.ErrorBody{Code: code, Message: message}})
}

func UpstreamStatus(status int) int {
	switch status {
	case http.StatusBadRequest, http.StatusUnprocessableEntity, http.StatusInternalServerError, http.StatusBadGateway:
		return status
	default:
		return http.StatusInternalServerError
	}
}
