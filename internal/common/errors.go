package common

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// APIVersionV1 is the API version stamped on every structured response/error,
// matching the legacy go-devops-gin response contract.
const APIVersionV1 = "v1"

// Status mirrors the legacy response status field.
type Status string

const (
	StatusSuccess Status = "Success"
	StatusFailure Status = "Failure"
)

// Kind namespaces an error to a resource domain. The two-digit module number
// derived from Kind forms the first part of an ErrorCode.
type Kind string

const (
	KindBase       Kind = "Base"
	KindAuth       Kind = "Auth"
	KindOrg        Kind = "Org"
	KindCatalog    Kind = "Catalog"
	KindService    Kind = "Service"
	KindComponent  Kind = "Component"
	KindCluster    Kind = "Cluster"
	KindEnv        Kind = "Environment"
	KindPipeline   Kind = "Pipeline"
	KindRun        Kind = "Run"
	KindPermission Kind = "Permission"
	KindArtifact   Kind = "Artifact"
)

// kindModule maps each Kind to its two-digit module number in the ErrorCode
// scheme (ERR.<mod><http><seq>). Catalog and Service share module 04 because
// the catalog is the service registry in this platform.
var kindModule = map[Kind]string{
	KindBase:       "01",
	KindAuth:       "02",
	KindOrg:        "03",
	KindCatalog:    "04",
	KindService:    "04",
	KindComponent:  "05",
	KindCluster:    "06",
	KindEnv:        "07",
	KindPipeline:   "08",
	KindRun:        "09",
	KindPermission: "10",
	KindArtifact:   "11",
}

// APIError is a coded error carrying a stable ErrorCode (e.g. ERR.01400001),
// an HTTP status, and a human-facing message. It implements error and is the
// value the hub's shared plumbing (RegisterCRUD, AbortWithError) returns,
// ported from the legacy go-devops-gin response contract.
type APIError struct {
	Kind         Kind   `json:"kind"`
	APIVersion   string `json:"apiVersion"`
	Status       Status `json:"status"`
	Code         int    `json:"code"`
	ErrorCode    string `json:"errorCode"`
	ErrorMessage string `json:"errorMessage"`
	Message      string `json:"message"`
}

// Error implements the error interface.
func (e *APIError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return e.ErrorMessage
}

// WithMessage overrides the human-facing message and returns e for chaining.
func (e *APIError) WithMessage(msg string) *APIError {
	e.Message = msg
	return e
}

// WithError folds an underlying error's message into Message (for debugging)
// and returns e for chaining.
func (e *APIError) WithError(err error) *APIError {
	if err != nil {
		e.Message = err.Error()
	}
	return e
}

// NewAPIError builds a coded error. seq is a 1-based counter within the
// (kind, httpCode) bucket so multiple distinct errors can share a code family
// (e.g. ERR.08400001, ERR.08400002 for two different pipeline 400s).
func NewAPIError(kind Kind, httpCode, seq int, msg string) *APIError {
	mod, ok := kindModule[kind]
	if !ok {
		mod = "01"
	}
	errorCode := fmt.Sprintf("ERR.%s%03d%03d", mod, httpCode, seq)
	return &APIError{
		Kind:         kind,
		APIVersion:   APIVersionV1,
		Status:       StatusFailure,
		Code:         httpCode,
		ErrorCode:    errorCode,
		ErrorMessage: msg,
	}
}

// Predefined base errors (ERR.01 series). Modules extend these with
// domain-specific constructors below.
var (
	ErrInternal     = NewAPIError(KindBase, http.StatusInternalServerError, 1, "Internal Server Error")
	ErrBadRequest   = NewAPIError(KindBase, http.StatusBadRequest, 1, "Invalid Parameter")
	ErrForbidden    = NewAPIError(KindBase, http.StatusForbidden, 1, "Forbidden")
	ErrUnauthorized = NewAPIError(KindBase, http.StatusUnauthorized, 1, "Unauthorized")
	ErrNotFound     = NewAPIError(KindBase, http.StatusNotFound, 1, "Not Found")
)

// DomainError is a convenience constructor for module-specific errors that
// follow the same family as the base errors. Example:
//
//	var ErrPipelineNotFound = DomainError(KindPipeline, http.StatusNotFound, 1, "Pipeline Not Found")
func DomainError(kind Kind, httpCode, seq int, msg string) *APIError {
	return NewAPIError(kind, httpCode, seq, msg)
}

// toAPIError normalizes any error into an *APIError. *APIError passes through;
// gorm.ErrRecordNotFound maps to ErrNotFound; everything else becomes
// ErrInternal so no raw stack trace leaks to the client.
func toAPIError(err error) *APIError {
	if err == nil {
		return nil
	}
	if ae, ok := err.(*APIError); ok {
		return ae
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound.WithError(err)
	}
	return ErrInternal.WithError(err)
}

// AbortWithError writes a structured error envelope and aborts the Gin chain.
// Use it for service/repository errors so clients get a stable ErrorCode.
func AbortWithError(c *gin.Context, err error) {
	ae := toAPIError(err)
	c.AbortWithStatusJSON(ae.Code, Envelope{ErrorCode: ae.ErrorCode, Error: ae.Error()})
}
