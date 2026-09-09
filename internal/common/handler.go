package common

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// CRUDService is the minimal interface RegisterCRUD needs from a module's
// service layer; every module's XxxService satisfies this automatically as
// long as its method signatures match — no explicit "implements" needed.
type CRUDService[T any] interface {
	Create(*T) error
	Get(uuid.UUID) (*T, error)
	Update(uuid.UUID, *T) error
	Delete(uuid.UUID) error
}

// RegisterCRUD wires up POST/GET/:id/PUT/:id/DELETE/:id for a single-entity
// resource at basePath (e.g. "/orgs"). Parent-scoped listing (almost every
// module needs one, e.g. components under a service) varies too much per
// module to generalize, so each module's handler adds that route itself.
func RegisterCRUD[T any](rg *gin.RouterGroup, basePath string, svc CRUDService[T]) {
	rg.POST(basePath, func(c *gin.Context) {
		var in T
		if err := c.ShouldBindJSON(&in); err != nil {
			AbortWithError(c, ErrBadRequest.WithError(err))
			return
		}
		if err := svc.Create(&in); err != nil {
			AbortWithError(c, err)
			return
		}
		Created(c, in)
	})

	rg.GET(basePath+"/:id", func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			AbortWithError(c, ErrBadRequest.WithError(err))
			return
		}
		item, err := svc.Get(id)
		if err != nil {
			AbortWithError(c, err)
			return
		}
		OK(c, item)
	})

	rg.PUT(basePath+"/:id", func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			AbortWithError(c, ErrBadRequest.WithError(err))
			return
		}
		var in T
		if err := c.ShouldBindJSON(&in); err != nil {
			AbortWithError(c, ErrBadRequest.WithError(err))
			return
		}
		if err := svc.Update(id, &in); err != nil {
			AbortWithError(c, err)
			return
		}
		OK(c, in)
	})

	rg.DELETE(basePath+"/:id", func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			AbortWithError(c, ErrBadRequest.WithError(err))
			return
		}
		if err := svc.Delete(id); err != nil {
			AbortWithError(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	})
}
