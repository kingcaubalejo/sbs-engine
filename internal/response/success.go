package response

import (
	"net/http"
)

func SuccessV1(w http.ResponseWriter, data interface{}) {
	LegacyJSON(w, data)
}

func SuccessV2(w http.ResponseWriter, message string, data interface{}) {
	JSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: message,
		Data:    data,
	})
}