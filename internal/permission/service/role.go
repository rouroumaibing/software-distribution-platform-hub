package service

import (
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/repository"
)

type RoleService struct{ repo *repository.RoleRepository }

func NewRoleService(repo *repository.RoleRepository) *RoleService { return &RoleService{repo: repo} }

func (s *RoleService) List() ([]models.Role, error) { return s.repo.List() }
