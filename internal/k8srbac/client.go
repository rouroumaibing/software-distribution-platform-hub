package k8srbac

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"time"

	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/utils/ptr"
)

// ClusterClient is the contract for talking to a target cluster. It is an
// interface so the hub logic can be unit-tested without a live apiserver.
type ClusterClient interface {
	// ApplyRole upserts a ClusterRole (clusterScoped) or Role (namespace).
	ApplyRole(ctx context.Context, name string, rules []rbacv1.PolicyRule, clusterScoped bool, namespace string) error
	// DeleteRole removes the role artifact.
	DeleteRole(ctx context.Context, name string, clusterScoped bool, namespace string) error
	// ApplyBinding upserts a ClusterRoleBinding / RoleBinding referencing roleRef,
	// with one subject (subjectKind ∈ {User,Group}; subjectName already resolved).
	ApplyBinding(ctx context.Context, name, roleRef string, subjectKind, subjectName string, clusterScoped bool, namespace string) error
	// DeleteBinding removes the binding artifact.
	DeleteBinding(ctx context.Context, name string, clusterScoped bool, namespace string) error
	// IssueUserKubeconfig generates a key + CSR (CN=commonName, O=org), approves
	// and waits for the target cluster to sign it, then returns a self-contained
	// kubeconfig (§8). The private key never leaves the hub.
	IssueUserKubeconfig(ctx context.Context, commonName, org string) (string, error)
	// RevokeUserSubject swaps the binding's subject name in place (§9): the old
	// cert no longer matches any binding → access revoked, structure preserved.
	RevokeUserSubject(ctx context.Context, bindingName, newSubjectName string) error
}

// clientSetClient is the client-go-backed ClusterClient.
type clientSetClient struct {
	cs        *kubernetes.Clientset
	restCfg   *rest.Config
	clusterCA []byte
	serverURL string
}

// NewClientFromKubeconfig builds a ClusterClient from a target cluster's
// kubeconfig YAML (§7: 复用 environment 凭据). The cluster CA + server are
// captured so IssueUserKubeconfig can assemble a fresh kubeconfig for a user.
func NewClientFromKubeconfig(kubeconfig string) (ClusterClient, error) {
	restCfg, err := clientcmd.RESTConfigFromKubeConfig([]byte(kubeconfig))
	if err != nil {
		return nil, fmt.Errorf("k8srbac: parse kubeconfig: %w", err)
	}
	cs, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("k8srbac: build client: %w", err)
	}
	rawCfg, err := clientcmd.Load([]byte(kubeconfig))
	if err != nil {
		return nil, fmt.Errorf("k8srbac: load kubeconfig: %w", err)
	}
	var ca []byte
	var server string
	for _, c := range rawCfg.Clusters {
		if c != nil {
			ca = c.CertificateAuthorityData
			server = c.Server
			break
		}
	}
	return &clientSetClient{cs: cs, restCfg: restCfg, clusterCA: ca, serverURL: server}, nil
}

func (c *clientSetClient) ApplyRole(ctx context.Context, name string, rules []rbacv1.PolicyRule, clusterScoped bool, namespace string) error {
	if clusterScoped {
		obj := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: name}, Rules: rules}
		return upsertClusterRole(ctx, c.cs, obj)
	}
	obj := &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}, Rules: rules}
	return upsertRole(ctx, c.cs, namespace, obj)
}

func (c *clientSetClient) DeleteRole(ctx context.Context, name string, clusterScoped bool, namespace string) error {
	if clusterScoped {
		return ignoreNotFound(c.cs.RbacV1().ClusterRoles().Delete(ctx, name, metav1.DeleteOptions{}))
	}
	return ignoreNotFound(c.cs.RbacV1().Roles(namespace).Delete(ctx, name, metav1.DeleteOptions{}))
}

func (c *clientSetClient) ApplyBinding(ctx context.Context, name, roleRef string, subjectKind, subjectName string, clusterScoped bool, namespace string) error {
	subject := rbacv1.Subject{Kind: subjectKind, Name: subjectName, APIGroup: rbacv1.GroupName}
	if clusterScoped {
		obj := &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: roleRef},
			Subjects:   []rbacv1.Subject{subject},
		}
		return upsertClusterRoleBinding(ctx, c.cs, obj)
	}
	obj := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: roleRef},
		Subjects:   []rbacv1.Subject{subject},
	}
	return upsertRoleBinding(ctx, c.cs, namespace, obj)
}

func (c *clientSetClient) DeleteBinding(ctx context.Context, name string, clusterScoped bool, namespace string) error {
	if clusterScoped {
		return ignoreNotFound(c.cs.RbacV1().ClusterRoleBindings().Delete(ctx, name, metav1.DeleteOptions{}))
	}
	return ignoreNotFound(c.cs.RbacV1().RoleBindings(namespace).Delete(ctx, name, metav1.DeleteOptions{}))
}

func (c *clientSetClient) IssueUserKubeconfig(ctx context.Context, commonName, org string) (string, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", fmt.Errorf("k8srbac: gen key: %w", err)
	}
	csrTmpl := x509.CertificateRequest{
		Subject:            pkix.Name{CommonName: commonName, Organization: []string{org}},
		SignatureAlgorithm: x509.SHA256WithRSA,
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &csrTmpl, key)
	if err != nil {
		return "", fmt.Errorf("k8srbac: create csr: %w", err)
	}
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})

	csrName := "sdp-" + commonName
	csr := &certificatesv1.CertificateSigningRequest{
		ObjectMeta: metav1.ObjectMeta{Name: csrName},
		Spec: certificatesv1.CertificateSigningRequestSpec{
			Request:           csrPEM,
			SignerName:        certificatesv1.KubeAPIServerClientSignerName,
			Usages:            []certificatesv1.KeyUsage{certificatesv1.UsageClientAuth},
			ExpirationSeconds: ptr.To(int32(86400 * 365)),
		},
	}
	created, err := c.cs.CertificatesV1().CertificateSigningRequests().Create(ctx, csr, metav1.CreateOptions{})
	if err != nil {
		return "", fmt.Errorf("k8srbac: create csr: %w", err)
	}
	// 自动批准（hub 受信签发通道）；目标集群签名控制器随后写入 status.certificate。
	created.Status.Conditions = append(created.Status.Conditions, certificatesv1.CertificateSigningRequestCondition{
		Type:    certificatesv1.CertificateApproved,
		Status:  corev1.ConditionTrue,
		Reason:  "HubAutoApproved",
		Message: "sdp hub auto-approve for cluster permission",
	})
	if _, err := c.cs.CertificatesV1().CertificateSigningRequests().UpdateApproval(ctx, created.Name, created, metav1.UpdateOptions{}); err != nil {
		return "", fmt.Errorf("k8srbac: approve csr: %w", err)
	}
	signed, err := c.waitForCertificate(ctx, csrName)
	if err != nil {
		return "", err
	}

	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return buildKubeconfig(c.serverURL, c.clusterCA, commonName, signed, keyPEM), nil
}

func (c *clientSetClient) waitForCertificate(ctx context.Context, csrName string) ([]byte, error) {
	deadline := time.Now().Add(30 * time.Second)
	for {
		csr, err := c.cs.CertificatesV1().CertificateSigningRequests().Get(ctx, csrName, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("k8srbac: get csr: %w", err)
		}
		if len(csr.Status.Certificate) > 0 {
			return csr.Status.Certificate, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("k8srbac: csr %s not signed within 30s", csrName)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (c *clientSetClient) RevokeUserSubject(ctx context.Context, bindingName, newSubjectName string) error {
	// 绑定名 = CRBName(bindingID)；集群权限绑定默认集群级，按 ClusterRoleBinding 处理。
	cr, err := c.cs.RbacV1().ClusterRoleBindings().Get(ctx, bindingName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("k8srbac: get binding %s: %w", bindingName, err)
	}
	if len(cr.Subjects) == 0 {
		return fmt.Errorf("k8srbac: binding %s has no subject", bindingName)
	}
	cr.Subjects[0].Name = newSubjectName
	if _, err := c.cs.RbacV1().ClusterRoleBindings().Update(ctx, cr, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("k8srbac: patch binding %s subject: %w", bindingName, err)
	}
	return nil
}

// ---- typed upsert helpers (create-or-update) ----

func upsertClusterRole(ctx context.Context, cs *kubernetes.Clientset, obj *rbacv1.ClusterRole) error {
	existing, err := cs.RbacV1().ClusterRoles().Get(ctx, obj.Name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			_, err = cs.RbacV1().ClusterRoles().Create(ctx, obj, metav1.CreateOptions{})
			return err
		}
		return err
	}
	existing.Rules = obj.Rules
	_, err = cs.RbacV1().ClusterRoles().Update(ctx, existing, metav1.UpdateOptions{})
	return err
}

func upsertRole(ctx context.Context, cs *kubernetes.Clientset, ns string, obj *rbacv1.Role) error {
	existing, err := cs.RbacV1().Roles(ns).Get(ctx, obj.Name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			_, err = cs.RbacV1().Roles(ns).Create(ctx, obj, metav1.CreateOptions{})
			return err
		}
		return err
	}
	existing.Rules = obj.Rules
	_, err = cs.RbacV1().Roles(ns).Update(ctx, existing, metav1.UpdateOptions{})
	return err
}

func upsertClusterRoleBinding(ctx context.Context, cs *kubernetes.Clientset, obj *rbacv1.ClusterRoleBinding) error {
	existing, err := cs.RbacV1().ClusterRoleBindings().Get(ctx, obj.Name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			_, err = cs.RbacV1().ClusterRoleBindings().Create(ctx, obj, metav1.CreateOptions{})
			return err
		}
		return err
	}
	existing.RoleRef = obj.RoleRef
	existing.Subjects = obj.Subjects
	_, err = cs.RbacV1().ClusterRoleBindings().Update(ctx, existing, metav1.UpdateOptions{})
	return err
}

func upsertRoleBinding(ctx context.Context, cs *kubernetes.Clientset, ns string, obj *rbacv1.RoleBinding) error {
	existing, err := cs.RbacV1().RoleBindings(ns).Get(ctx, obj.Name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			_, err = cs.RbacV1().RoleBindings(ns).Create(ctx, obj, metav1.CreateOptions{})
			return err
		}
		return err
	}
	existing.RoleRef = obj.RoleRef
	existing.Subjects = obj.Subjects
	_, err = cs.RbacV1().RoleBindings(ns).Update(ctx, existing, metav1.UpdateOptions{})
	return err
}

func ignoreNotFound(err error) error {
	if err == nil {
		return nil
	}
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

// buildKubeconfig assembles a self-contained kubeconfig for an issued user cert.
func buildKubeconfig(server string, ca []byte, user string, certPEM, keyPEM []byte) string {
	return fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: sdp-target
  cluster:
    server: %s
    certificate-authority-data: %s
contexts:
- name: sdp-target
  context:
    cluster: sdp-target
    user: %s
current-context: sdp-target
users:
- name: %s
  user:
    client-certificate-data: %s
    client-key-data: %s
`, server, base64.StdEncoding.EncodeToString(ca), user, user,
		base64.StdEncoding.EncodeToString(certPEM), base64.StdEncoding.EncodeToString(keyPEM))
}
