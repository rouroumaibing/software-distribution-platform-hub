package common

import (
	"strconv"

	"github.com/gin-gonic/gin"
)

// Pagination holds parsed page/pageSize query params, shared by every
// module's List handler so pagination behaves identically across the API.
type Pagination struct {
	Page     int
	PageSize int
}

func (p Pagination) Offset() int { return (p.Page - 1) * p.PageSize }
func (p Pagination) Limit() int  { return p.PageSize }

// ParsePagination reads ?page=&pageSize= from the request, applying sane
// defaults and an upper bound so a client can't request an unbounded page.
func ParsePagination(c *gin.Context) Pagination {
	page, err := strconv.Atoi(c.Query("page"))
	if err != nil || page < 1 {
		page = 1
	}
	pageSize, err := strconv.Atoi(c.Query("pageSize"))
	if err != nil || pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return Pagination{Page: page, PageSize: pageSize}
}
