package common

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Envelope is the standard response shape every handler returns, so the
// console's API client can rely on one consistent structure. Success responses
// carry Data; failures carry Error and, when produced by the shared error
// layer, a stable ErrorCode (e.g. ERR.01400001) and optionally a structured
// Reasons list (e.g. residual resources blocking a delete).
type Envelope struct {
	Data      any      `json:"data,omitempty"`
	Error     string   `json:"error,omitempty"`
	ErrorCode string   `json:"errorCode,omitempty"`
	Reasons   []string `json:"reasons,omitempty"`
}

// PagedData wraps a List response with total count for pagination UI.
type PagedData struct {
	Items    any   `json:"items"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"pageSize"`
}

func OK(c *gin.Context, data any) { c.JSON(http.StatusOK, Envelope{Data: data}) }

// OKNoEscape is OK for payloads that carry URLs. gin's c.JSON html-escapes
// '&' into the literal text `\u0026`, which is valid JSON but corrupts the
// query string for shell clients that extract the URL with sed/grep instead
// of a JSON parser — a signed artifact URL then 403s on a garbage signature
// (2026-09-26 E2E 实测）。Encoding with SetEscapeHTML(false) keeps '&' literal
// and the JSON body stays spec-valid.
func OKNoEscape(c *gin.Context, data any) {
	c.Header("Content-Type", "application/json; charset=utf-8")
	c.Status(http.StatusOK)
	enc := json.NewEncoder(c.Writer)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(Envelope{Data: data})
}

func OKPaged(c *gin.Context, items any, total int64, p Pagination) {
	c.JSON(http.StatusOK, Envelope{Data: PagedData{Items: items, Total: total, Page: p.Page, PageSize: p.PageSize}})
}

func Created(c *gin.Context, data any) { c.JSON(http.StatusCreated, Envelope{Data: data}) }

// Fail writes an error envelope at the given HTTP status. If err is a coded
// *APIError, its ErrorCode and Reasons are included so clients get a stable
// machine-readable code and, where present, a structured rejection reason list.
func Fail(c *gin.Context, status int, err error) {
	code := ""
	var reasons []string
	if ae, ok := err.(*APIError); ok {
		code = ae.ErrorCode
		reasons = ae.Reasons
	}
	c.JSON(status, Envelope{Error: err.Error(), ErrorCode: code, Reasons: reasons})
}
