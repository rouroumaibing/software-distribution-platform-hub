// Package k8srbac implements the K8s RBAC authorization layer (ACCESS-CONTROL
// §7-§9): hub 复用目标集群（environment）的 kubeconfig 凭据直连下发 ClusterRole/
// Role + ClusterRoleBinding/RoleBinding，并为 User 主体经目标集群 CSR API 签发
// kubeconfig（hub 不持目标集群 CA）。Group/Org 主体以 k8s Group subject（KC 组路径）
// 下 CRB，成员经自身 OIDC token 的 groups 继承，不单独发证（§8 的 `<id>-<hash>` 仅
// 用于 User 主体的证书 CN / CRB subject，确保与证书逐字一致）。
package k8srbac

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"

	rbacv1 "k8s.io/api/rbac/v1"
	"gorm.io/datatypes"
)

// Subject kind（§7.1）：hub 侧主体类型。
const (
	SubjectUser  = "user"
	SubjectGroup = "group"
	SubjectOrg   = "org"
)

// Cluster role（§7.1 预置五角色）。
const (
	RoleAdmin     = "admin"
	RoleOps       = "ops"
	RoleDev       = "dev"
	RoleReadonly  = "readonly"
	RoleCustom    = "custom"
)

// Resource class（§7.2 五类）。
const (
	ResPods               = "pods"
	ResDeployments        = "deployments"
	ResServices           = "services"
	ResConfigmapsSecrets  = "configmaps-secrets"
	ResNamespaces         = "namespaces"
)

// Operation（§7.2 两类）。
const (
	OpManage = "manage"
	OpQuery  = "query"
)

// resourceClassToKinds maps a high-level resource class to concrete K8s resource
// kinds (used by the predefined roles + custom role generation).
var resourceClassToKinds = map[string][]string{
	ResPods:              {"pods"},
	ResDeployments:       {"deployments", "replicasets", "statefulsets", "daemonsets", "jobs", "cronjobs"},
	ResServices:          {"services", "endpoints", "ingresses"},
	ResConfigmapsSecrets: {"configmaps", "secrets"},
	ResNamespaces:        {"namespaces", "limitranges", "resourcequotas"},
}

// operationVerbs maps an operation to its K8s verbs (§7.2).
func operationVerbs(op string) []string {
	switch op {
	case OpManage:
		return []string{"create", "update", "patch", "delete", "deletecollection"}
	case OpQuery:
		return []string{"get", "list", "watch"}
	default:
		return nil
	}
}

// VerbsFor expands a set of resource classes + operations into concrete K8s
// PolicyRules. Pure function, unit-tested without a cluster.
func VerbsFor(resourceClasses, operations []string) []rbacv1.PolicyRule {
	kinds := map[string]struct{}{}
	for _, rc := range resourceClasses {
		for _, k := range resourceClassToKinds[rc] {
			kinds[k] = struct{}{}
		}
	}
	verbs := map[string]struct{}{}
	for _, op := range operations {
		for _, v := range operationVerbs(op) {
			verbs[v] = struct{}{}
		}
	}
	if len(kinds) == 0 || len(verbs) == 0 {
		return nil
	}
	ks := make([]string, 0, len(kinds))
	for k := range kinds {
		ks = append(ks, k)
	}
	vs := make([]string, 0, len(verbs))
	for v := range verbs {
		vs = append(vs, v)
	}
	return []rbacv1.PolicyRule{{
		APIGroups: []string{""},
		Resources: ks,
		Verbs:     vs,
	}}
}

// RoleTemplate builds the PolicyRules for a role. Predefined roles return a fixed
// rule set; custom assembles rules from the supplied resource classes + operations
// (§7.1 / §7.2). Pure function.
func RoleTemplate(role string, resourceClasses, operations []string) []rbacv1.PolicyRule {
	switch role {
	case RoleAdmin:
		// 全资源 + RBAC 资源（§7.1 admin）。
		return []rbacv1.PolicyRule{
			{APIGroups: []string{"*"}, Resources: []string{"*"}, Verbs: []string{"*"}},
			{APIGroups: []string{"rbac.authorization.k8s.io"}, Resources: []string{"*"}, Verbs: []string{"*"}},
		}
	case RoleOps:
		// 工作负载 + 网络，全动词（§7.1 ops）。
		return []rbacv1.PolicyRule{{
			APIGroups: []string{""},
			Resources: []string{"pods", "deployments", "replicasets", "statefulsets", "daemonsets", "jobs", "cronjobs", "services", "endpoints", "ingresses", "networkpolicies"},
			Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete", "deletecollection"},
		}}
	case RoleDev:
		// 工作负载（namespaced）全动词（§7.1 dev）。
		return []rbacv1.PolicyRule{{
			APIGroups: []string{""},
			Resources: []string{"pods", "deployments", "replicasets", "statefulsets", "daemonsets", "jobs", "cronjobs"},
			Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete", "deletecollection"},
		}}
	case RoleReadonly:
		// 全资源只读（§7.1 readonly）。
		return []rbacv1.PolicyRule{{
			APIGroups: []string{"*"},
			Resources: []string{"*"},
			Verbs:     []string{"get", "list", "watch"},
		}}
	case RoleCustom:
		return VerbsFor(resourceClasses, operations)
	default:
		return nil
	}
}

// CRBName returns the stable CRB/RoleBinding resource name for a binding (§8):
// `<id>-cert`. The binding's k8s subject name is derived separately so a cert
// rotation only swaps the subject, not the binding name.
func CRBName(bindingID string) string {
	return bindingID + "-cert"
}

// K8sSubjectName returns the k8s RBAC subject name for a binding (§7.1 / §8):
//   - User:  `<subjectID>-<certHash>` —— 与签发的证书 CN 逐字一致，认证才通过。
//   - Group/Org: KC 组路径（subjectName），成员经 OIDC token groups 继承，不发证。
func K8sSubjectName(subjectKind, subjectID, subjectName, certHash string) (string, string) {
	switch subjectKind {
	case SubjectUser:
		name := fmt.Sprintf("%s-%s", subjectID, certHash)
		return rbacv1.UserKind, name
	case SubjectGroup, SubjectOrg:
		return rbacv1.GroupKind, subjectName
	default:
		return "", ""
	}
}

// GenerateHash returns a one-time random hex value used as the cert discriminator
// (§8 `<hash>`：进证书 O 字段 + 拼入 User 主体 CN/subject 名；§9 吊销 = 换新 hash
// 换 subject)。16 bytes → 32 hex chars，足够区分且不可枚举。
func GenerateHash() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("k8srbac: generate hash: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// mustJSON marshals v into a datatypes.JSON column (custom role resources/operations).
func mustJSON(v interface{}) datatypes.JSON {
	b, err := json.Marshal(v)
	if err != nil {
		return datatypes.JSON("[]")
	}
	return datatypes.JSON(b)
}

// parseJSON unmarshals a datatypes.JSON column back into out ([]string etc.).
func parseJSON(j datatypes.JSON, out interface{}) error {
	if len(j) == 0 {
		return nil
	}
	return json.Unmarshal(j, out)
}
