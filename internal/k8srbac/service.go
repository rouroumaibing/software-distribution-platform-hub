package k8srbac

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// KubeconfigResolver resolves a target cluster's kubeconfig from an environment
// ID. Implemented at the wiring layer by composing the environment + credential
// services (environment.KubeCredRef → credentials 表 kubeconfig Value).
type KubeconfigResolver interface {
	Resolve(ctx context.Context, environmentID uuid.UUID) (kubeconfig string, err error)
}

// CreateInput is the payload for creating a cluster permission (§7 / §8).
type CreateInput struct {
	EnvironmentID uuid.UUID `json:"environmentId"`
	SubjectKind   string   `json:"subjectKind"` // user|group|org
	SubjectID     string   `json:"subjectId"`   // hub-side ID
	SubjectName   string   `json:"subjectName"` // KC group path / username (Group/Org subject name)
	Role          string   `json:"role"`        // admin|ops|dev|readonly|custom
	ClusterScoped bool     `json:"clusterScoped"`
	Namespace     string   `json:"namespace"`
	Resources     []string `json:"resources"` // custom only
	Operations    []string `json:"operations"` // custom only
}

// Service orchestrates the K8s RBAC layer: resolves target-cluster credentials,
// applies/revokes bindings, and issues user kubeconfigs. The client factory is
// injectable so the orchestration can be unit-tested without a live cluster.
type Service struct {
	repo          *Repository
	resolver      KubeconfigResolver
	clientFactory func(kubeconfig string) (ClusterClient, error)
}

func NewService(repo *Repository, resolver KubeconfigResolver) *Service {
	return &Service{repo: repo, resolver: resolver, clientFactory: NewClientFromKubeconfig}
}

func (s *Service) clientForEnv(ctx context.Context, envID uuid.UUID) (ClusterClient, error) {
	kc, err := s.resolver.Resolve(ctx, envID)
	if err != nil {
		return nil, fmt.Errorf("k8srbac: resolve kubeconfig: %w", err)
	}
	return s.clientFactory(kc)
}

// Create provisions a cluster permission end-to-end (§7 + §8):
//  1. resolve target cluster kubeconfig + build client;
//  2. User 主体先生成 cert hash；
//  3. 下发 ClusterRole/Role（角色模板）+ ClusterRoleBinding/RoleBinding；
//  4. User 主体经 CSR 签发 kubeconfig 一并返回。
//
// 任一集群侧失败 → 回滚（删 hub 记录 + 清已下发的集群资源），避免孤儿授权。
func (s *Service) Create(ctx context.Context, in *CreateInput) (*ClusterBinding, string, error) {
	if err := validateCreate(in); err != nil {
		return nil, "", err
	}
	client, err := s.clientForEnv(ctx, in.EnvironmentID)
	if err != nil {
		return nil, "", err
	}

	b := &ClusterBinding{
		ID:            uuid.New(),
		EnvironmentID: in.EnvironmentID,
		SubjectKind:   in.SubjectKind,
		SubjectID:     in.SubjectID,
		SubjectName:   in.SubjectName,
		Role:          in.Role,
		ClusterScoped: in.ClusterScoped,
		Namespace:     in.Namespace,
		Status:        StatusActive,
	}
	if in.Role == RoleCustom {
		b.Resources = mustJSON(in.Resources)
		b.Operations = mustJSON(in.Operations)
	}
	if in.SubjectKind == SubjectUser {
		hash, err := GenerateHash()
		if err != nil {
			return nil, "", err
		}
		b.CertHash = hash
	}

	rules := RoleTemplate(in.Role, in.Resources, in.Operations)
	roleName := b.ID.String() + "-role"
	bindingName := CRBName(b.ID.String())
	kind, subjName := K8sSubjectName(b.SubjectKind, b.SubjectID, b.SubjectName, b.CertHash)

	// 先下发集群资源，再落 hub 记录；任一失败回滚集群侧。
	if err := client.ApplyRole(ctx, roleName, rules, b.ClusterScoped, b.Namespace); err != nil {
		return nil, "", fmt.Errorf("k8srbac: apply role: %w", err)
	}
	if err := client.ApplyBinding(ctx, bindingName, roleName, kind, subjName, b.ClusterScoped, b.Namespace); err != nil {
		_ = client.DeleteRole(ctx, roleName, b.ClusterScoped, b.Namespace)
		return nil, "", fmt.Errorf("k8srbac: apply binding: %w", err)
	}
	if err := s.repo.Create(b); err != nil {
		_ = client.DeleteBinding(ctx, bindingName, b.ClusterScoped, b.Namespace)
		_ = client.DeleteRole(ctx, roleName, b.ClusterScoped, b.Namespace)
		return nil, "", fmt.Errorf("k8srbac: persist binding: %w", err)
	}

	var issuedKubeconfig string
	if b.SubjectKind == SubjectUser {
		commonName := fmt.Sprintf("%s-%s", b.SubjectID, b.CertHash)
		issued, err := client.IssueUserKubeconfig(ctx, commonName, b.CertHash)
		if err != nil {
			// 证书签发失败：回滚集群绑定 + hub 记录，保证无孤儿授权。
			_ = client.DeleteBinding(ctx, bindingName, b.ClusterScoped, b.Namespace)
			_ = client.DeleteRole(ctx, roleName, b.ClusterScoped, b.Namespace)
			_ = s.repo.Delete(b.ID)
			return nil, "", fmt.Errorf("k8srbac: issue kubeconfig: %w", err)
		}
		issuedKubeconfig = issued
	}
	return b, issuedKubeconfig, nil
}

// Revoke revokes a cluster permission (§9):
//   - User 主体：换 cert hash + 重新签发 kubeconfig + PATCH CRB subject（旧证书无处匹配→失效）。
//   - Group/Org 主体：直接删除集群绑定（无证书转动概念），标记 revoked。
func (s *Service) Revoke(ctx context.Context, id uuid.UUID) (*ClusterBinding, string, error) {
	b, err := s.repo.Get(id)
	if err != nil {
		return nil, "", err
	}
	if b.Status == StatusRevoked {
		return b, "", nil
	}
	client, err := s.clientForEnv(ctx, b.EnvironmentID)
	if err != nil {
		return nil, "", err
	}
	bindingName := CRBName(b.ID.String())
	roleName := b.ID.String() + "-role"

	if b.SubjectKind == SubjectUser {
		newHash, err := GenerateHash()
		if err != nil {
			return nil, "", err
		}
		newCommon := fmt.Sprintf("%s-%s", b.SubjectID, newHash)
		issued, err := client.IssueUserKubeconfig(ctx, newCommon, newHash)
		if err != nil {
			return nil, "", fmt.Errorf("k8srbac: re-issue kubeconfig: %w", err)
		}
		newSubject := fmt.Sprintf("%s-%s", b.SubjectID, newHash)
		if err := client.RevokeUserSubject(ctx, bindingName, newSubject); err != nil {
			return nil, "", fmt.Errorf("k8srbac: rotate subject: %w", err)
		}
		b.CertHash = newHash
		if err := s.repo.Update(b); err != nil {
			return nil, "", err
		}
		// §9：旧证书因 subject 不匹配而失效，新证书为当前有效凭据；绑定结构保留（不删 CRB），
		// grant 仍为 active。若需彻底移除访问，调用 Delete。
		return b, issued, nil
	}

	// Group/Org：删除集群绑定 + 标记 revoked。
	if err := client.DeleteBinding(ctx, bindingName, b.ClusterScoped, b.Namespace); err != nil {
		return nil, "", fmt.Errorf("k8srbac: delete binding: %w", err)
	}
	_ = client.DeleteRole(ctx, roleName, b.ClusterScoped, b.Namespace)
	if err := s.repo.SetStatus(b.ID, StatusRevoked); err != nil {
		return nil, "", err
	}
	return b, "", nil
}

// Delete hard-removes a cluster permission: cluster artifacts + hub row.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	b, err := s.repo.Get(id)
	if err != nil {
		return err
	}
	client, err := s.clientForEnv(ctx, b.EnvironmentID)
	if err != nil {
		return err
	}
	bindingName := CRBName(b.ID.String())
	roleName := b.ID.String() + "-role"
	_ = client.DeleteBinding(ctx, bindingName, b.ClusterScoped, b.Namespace)
	_ = client.DeleteRole(ctx, roleName, b.ClusterScoped, b.Namespace)
	return s.repo.Delete(b.ID)
}

func (s *Service) ListByEnvironment(ctx context.Context, envID uuid.UUID) ([]ClusterBinding, error) {
	return s.repo.ListByEnvironment(envID)
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (*ClusterBinding, error) {
	return s.repo.Get(id)
}

func validateCreate(in *CreateInput) error {
	switch in.SubjectKind {
	case SubjectUser, SubjectGroup, SubjectOrg:
	default:
		return fmt.Errorf("k8srbac: invalid subjectKind %q", in.SubjectKind)
	}
	switch in.Role {
	case RoleAdmin, RoleOps, RoleDev, RoleReadonly:
	case RoleCustom:
		if len(in.Resources) == 0 || len(in.Operations) == 0 {
			return fmt.Errorf("k8srbac: custom role requires resources and operations")
		}
	default:
		return fmt.Errorf("k8srbac: invalid role %q", in.Role)
	}
	if !in.ClusterScoped && in.Namespace == "" {
		return fmt.Errorf("k8srbac: namespace-scoped binding requires namespace")
	}
	if in.SubjectKind != SubjectUser && in.SubjectName == "" {
		return fmt.Errorf("k8srbac: group/org subject requires subjectName (KC group path)")
	}
	return nil
}
