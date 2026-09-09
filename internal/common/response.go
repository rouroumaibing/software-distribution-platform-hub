package common

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Envelope is the standard response shape every handler returns, so the
// console's API client can rely on one consistent structure. Success responses
// carry Data; failures carry Error and, when produced by the shared error
// layer, a stable ErrorCode (e.g. ERR.01400001).
type Envelope struct {
	Data      any    `json:"data,omitempty"`
	Error     string `json:"error,omitempty"`
	ErrorCode string `json:"errorCode,omitempty"`
}

// PagedData wraps a List response with total count for pagination UI.
type PagedData struct {
	Items    any   `json:"items"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"pageSize"`
}

func OK(c *gin.Context, data any) { c.JSON(http.StatusOK, Envelope{Data: data}) }

func OKPaged(c *gin.Context, items any, total int64, p Pagination) {
	c.JSON(http.StatusOK, Envelope{Data: PagedData{Items: items, Total: total, Page: p.Page, PageSize: p.PageSize}})
}

func Created(c *gin.Context, data any) { c.JSON(http.StatusCreated, Envelope{Data: data}) }

// Fail writes an error envelope at the given HTTP status. If err is a coded
// *APIError, itsErrorCode is included so clients get a stable machine-readable
// code; otherwise only the human message is returned.
func Fail(c *gin.Context, status int, err error) {
	code := ""
	if ae, ok := err.(*APIError); ok {
		code = ae.ErrorCode
	}
	c.JSON(status, Envelope{Error: err.Error(), ErrorCode: code})
}
