package handler

import (
	"strings"
	"testing"
)

// Regression: reHasClusters previously lacked the (?m) flag, so a kubeconfig
// whose "clusters:" key was not preceded only by whitespace (i.e. any real
// kubeconfig with apiVersion/kind/current-context above it) was reported as
// missing the clusters section.
func TestParseKubeconfig_ClustersAnywhere(t *testing.T) {
	raw := `apiVersion: v1
kind: Config
current-context: kind-sdp-dev
clusters:
- name: c1
  cluster:
    server: https://127.0.0.1:6443
contexts:
- name: kind-sdp-dev
  context:
    cluster: c1
    user: u1
users:
- name: u1
  user:
    token: abc
`
	res := parseKubeconfig(raw)
	if res.Server != "https://127.0.0.1:6443" {
		t.Errorf("server = %q, want https://127.0.0.1:6443", res.Server)
	}
	if res.CurrentContext != "kind-sdp-dev" {
		t.Errorf("currentContext = %q, want kind-sdp-dev", res.CurrentContext)
	}
	if res.AuthMethod != "token" {
		t.Errorf("authMethod = %q, want token", res.AuthMethod)
	}
	for _, e := range res.Errors {
		if e == "不是合法 kubeconfig：缺少 clusters 段" {
			t.Errorf("clusters section present but reported missing; errors = %v", res.Errors)
		}
	}
}

func TestParseKubeconfig_RejectsExecCredential(t *testing.T) {
	raw := `apiVersion: v1
kind: Config
clusters:
- name: c1
  cluster:
    server: https://127.0.0.1:6443
users:
- name: u1
  user:
    exec:
      apiVersion: client.authentication.k8s.io/v1beta1
      command: my-plugin
`
	res := parseKubeconfig(raw)
	found := false
	for _, e := range res.Errors {
		if strings.Contains(e, "平台不支持 exec") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected exec-credential rejection error, got %v", res.Errors)
	}
}

func TestParseKubeconfig_MissingClustersReported(t *testing.T) {
	res := parseKubeconfig("apiVersion: v1\nkind: Config\n")
	found := false
	for _, e := range res.Errors {
		if e == "不是合法 kubeconfig：缺少 clusters 段" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected missing-clusters error, got %v", res.Errors)
	}
}
