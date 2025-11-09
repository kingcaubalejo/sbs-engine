package response

import (
	"net/http"
)

func Error(w http.ResponseWriter, statusCode int, errMsg string) {
	JSON(w, statusCode, APIResponse{
		Success: false,
		Error:   errMsg,
	})
}