package k8srbac

import (
	"encoding/hex"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
)

func TestVerbsFor(t *testing.T) {
	got := VerbsFor([]string{ResPods}, []string{OpQuery})
	if len(got) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(got))
	}
	rule := got[0]
	wantVerbs := map[string]bool{"get": true, "list": true, "watch": true}
	if len(rule.Verbs) != len(wantVerbs) {
		t.Fatalf("expected verbs %v, got %v", wantVerbs, rule.Verbs)
	}
	for _, v := range rule.Verbs {
		if !wantVerbs[v] {
			t.Errorf("unexpected verb %q", v)
		}
	}
	if rule.Resources[0] != "pods" {
		t.Errorf("expected resource pods, got %v", rule.Resources)
	}

	// manage 含全部动词。
	got = VerbsFor([]string{ResServices}, []string{OpManage})
	if len(got) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(got))
	}
	wantManage := map[string]bool{"create": true, "update": true, "patch": true, "delete": true, "deletecollection": true}
	if len(got[0].Verbs) != len(wantManage) {
		t.Fatalf("expected manage verbs %v, got %v", wantManage, got[0].Verbs)
	}

	// 空输入 → 无规则。
	if r := VerbsFor(nil, []string{OpQuery}); r != nil {
		t.Errorf("expected nil for empty resources, got %v", r)
	}
}

func TestRoleTemplate(t *testing.T) {
	cases := []struct {
		role    string
		classes []string
		ops     []string
		check   func(t *testing.T, rules []rbacv1.PolicyRule)
	}{
		{RoleAdmin, nil, nil, func(t *testing.T, r []rbacv1.PolicyRule) {
			if len(r) != 2 {
				t.Errorf("admin: expected 2 rules, got %d", len(r))
			}
		}},
		{RoleReadonly, nil, nil, func(t *testing.T, r []rbacv1.PolicyRule) {
			if r[0].Verbs[0] != "get" {
				t.Errorf("readonly: expected get, got %v", r[0].Verbs)
			}
			if r[0].APIGroups[0] != "*" {
				t.Errorf("readonly: expected * apiGroups, got %v", r[0].APIGroups)
			}
		}},
		{RoleCustom, []string{ResPods, ResDeployments}, []string{OpQuery, OpManage}, func(t *testing.T, r []rbacv1.PolicyRule) {
			if len(r) != 1 {
				t.Fatalf("custom: expected 1 rule, got %d", len(r))
			}
			// pods + 6 deployment kinds
			if len(r[0].Resources) != 7 {
				t.Errorf("custom: expected 7 resources, got %v", r[0].Resources)
			}
			if len(r[0].Verbs) != 8 {
				t.Errorf("custom: expected 8 verbs, got %v", r[0].Verbs)
			}
		}},
	}
	for _, tc := range cases {
		rules := RoleTemplate(tc.role, tc.classes, tc.ops)
		if rules == nil {
			t.Fatalf("%s: nil rules", tc.role)
		}
		tc.check(t, rules)
	}

	// 未知角色 → nil。
	if RoleTemplate("bogus", nil, nil) != nil {
		t.Errorf("unknown role should yield nil rules")
	}
}

func TestCRBName(t *testing.T) {
	if got := CRBName("abc-123"); got != "abc-123-cert" {
		t.Errorf("CRBName = %q, want abc-123-cert", got)
	}
}

func TestK8sSubjectName(t *testing.T) {
	// User：`<subjectID>-<hash>` + UserKind。
	kind, name := K8sSubjectName(SubjectUser, "uid-1", "", "deadbeef")
	if kind != rbacv1.UserKind || name != "uid-1-deadbeef" {
		t.Errorf("user subject = (%q,%q), want (User,uid-1-deadbeef)", kind, name)
	}
	// Group：KC 组路径 + GroupKind。
	kind, name = K8sSubjectName(SubjectGroup, "gid-1", "/acme/team", "hash")
	if kind != rbacv1.GroupKind || name != "/acme/team" {
		t.Errorf("group subject = (%q,%q), want (Group,/acme/team)", kind, name)
	}
	// Org：同 Group。
	kind, name = K8sSubjectName(SubjectOrg, "oid-1", "/acme", "hash")
	if kind != rbacv1.GroupKind || name != "/acme" {
		t.Errorf("org subject = (%q,%q), want (Group,/acme)", kind, name)
	}
}

func TestGenerateHash(t *testing.T) {
	h, err := GenerateHash()
	if err != nil {
		t.Fatalf("GenerateHash error: %v", err)
	}
	if len(h) != 32 {
		t.Errorf("expected 32 hex chars, got %d (%q)", len(h), h)
	}
	if _, err := hex.DecodeString(h); err != nil {
		t.Errorf("hash not valid hex: %v", err)
	}
	h2, _ := GenerateHash()
	if h == h2 {
		t.Errorf("expected unique hashes across calls")
	}
}
