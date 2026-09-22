package repository

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/org/models"
)

type OrgRepository struct{ *common.Repository[models.Org] }

func NewOrgRepository(db *gorm.DB) *OrgRepository {
	return &OrgRepository{common.NewRepository[models.Org](db)}
}

// IDsBySlugs resolves org aliases (orgs.slug) to their ids in one query. It is
// the join between the token's org carrier — `/org:<slug>` groups, the D1
// default (ACCOUNT-PERMISSION-MODEL §2.3) — and the uuid columns of
// resource_ownership (§3), which is keyed by org id.
//
// Unknown aliases are simply absent from the result map, never invented: the
// token can name an org this hub has never seen (deleted here, or created in
// Keycloak only), and a made-up id would silently widen access. The caller
// decides what an unresolvable alias means.
func (r *OrgRepository) IDsBySlugs(slugs []string) (map[string]uuid.UUID, error) {
	out := make(map[string]uuid.UUID, len(slugs))
	if len(slugs) == 0 {
		return out, nil
	}
	orgs, err := r.ListWhere("slug IN ?", slugs)
	if err != nil {
		return nil, err
	}
	for i := range orgs {
		out[orgs[i].Slug] = orgs[i].ID
	}
	return out, nil
}
