package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/environment/models"
	targetmodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/target/models"
)

// fakeEnvStore is the DB-free stand-in for EnvironmentStore.
type fakeEnvStore struct {
	byID      map[uuid.UUID]*models.Environment
	created   []*models.Environment
	updated   []*models.Environment
	deleted   []uuid.UUID
	updateErr error
	deleteErr error
}

func newFakeEnvStore(envs ...*models.Environment) *fakeEnvStore {
	byID := map[uuid.UUID]*models.Environment{}
	for _, e := range envs {
		byID[e.ID] = e
	}
	return &fakeEnvStore{byID: byID}
}

func (f *fakeEnvStore) Create(e *models.Environment) error {
	f.created = append(f.created, e)
	return nil
}

func (f *fakeEnvStore) GetByID(id uuid.UUID) (*models.Environment, error) {
	if e, ok := f.byID[id]; ok {
		return e, nil
	}
	return nil, common.ErrResourceNotFound
}

func (f *fakeEnvStore) FindByComponentID(uuid.UUID, common.Pagination) ([]models.Environment, int64, error) {
	return nil, 0, nil
}

func (f *fakeEnvStore) Update(e *models.Environment) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	f.updated = append(f.updated, e)
	f.byID[e.ID] = e
	return nil
}

func (f *fakeEnvStore) Delete(id uuid.UUID) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deleted = append(f.deleted, id)
	return nil
}

type fakeTargetLookup struct {
	target *targetmodels.Target
	err    error
}

func (f *fakeTargetLookup) GetByID(uuid.UUID) (*targetmodels.Target, error) {
	return f.target, f.err
}

type fakeCounter struct {
	n     int64
	err   error
	calls []uuid.UUID
}

func (f *fakeCounter) CountByEnvironment(id uuid.UUID) (int64, error) {
	f.calls = append(f.calls, id)
	return f.n, f.err
}

func agentEnv() *models.Environment {
	return &models.Environment{
		ComponentID: uuid.New(),
		Key:         "prod",
		Name:        "prod",
		TargetID:    uuid.New(),
		Namespace:   "org-comp-prod",
		Access:      models.EnvAccessAgent,
		Status:      models.EnvStatusVerified,
	}
}

// --- Delete: audit, never block (DELETE-CONTRACT §6.4 #8) ------------------

func TestDelete_CountsOverridesButNeverBlocks(t *testing.T) {
	env := agentEnv()
	store := newFakeEnvStore(env)
	counter := &fakeCounter{n: 3}
	svc := NewEnvironmentService(store, &fakeTargetLookup{}, counter)

	if err := svc.Delete(env.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(counter.calls) != 1 || counter.calls[0] != env.ID {
		t.Errorf("counter consulted %v times for %v, want exactly once for the deleted env", len(counter.calls), counter.calls)
	}
	if len(store.deleted) != 1 || store.deleted[0] != env.ID {
		t.Errorf("deleted = %v, want [%s] — residual overrides are an audit warning, not a veto", store.deleted, env.ID)
	}
}

func TestDelete_CounterFailureStillDeletes(t *testing.T) {
	// A failing count must not turn a permitted delete into an error: the
	// counter exists to *warn*, so losing it degrades the warning, not the
	// operation.
	env := agentEnv()
	store := newFakeEnvStore(env)
	counter := &fakeCounter{err: errors.New("count query blew up")}
	svc := NewEnvironmentService(store, &fakeTargetLookup{}, counter)

	if err := svc.Delete(env.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(store.deleted) != 1 {
		t.Error("the delete must still go through when the audit count fails")
	}
}

func TestDelete_NilCounterIsSafe(t *testing.T) {
	env := agentEnv()
	store := newFakeEnvStore(env)
	svc := NewEnvironmentService(store, &fakeTargetLookup{}, nil)

	if err := svc.Delete(env.ID); err != nil {
		t.Fatalf("Delete with no counter: %v", err)
	}
	if len(store.deleted) != 1 {
		t.Error("an unwired counter must not stop the delete")
	}
}

func TestDelete_StoreFailureSurfaces(t *testing.T) {
	store := newFakeEnvStore(agentEnv())
	store.deleteErr = errors.New("fk violation")
	svc := NewEnvironmentService(store, &fakeTargetLookup{}, &fakeCounter{})

	if err := svc.Delete(uuid.New()); err == nil {
		t.Fatal("a store failure must surface, not be swallowed")
	}
}

// --- Create / Update: status machine (§7.12.6) -----------------------------

func TestCreate_DerivesStatusFromAccess(t *testing.T) {
	store := newFakeEnvStore()
	svc := NewEnvironmentService(store, &fakeTargetLookup{}, nil)

	configured := agentEnv()
	configured.ID = uuid.New()
	configured.Status = models.EnvStatusVerified // must be overwritten
	if err := svc.Create(configured); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if configured.Status != models.EnvStatusConfiguredUnverified {
		t.Errorf("status = %q, want configured_unverified for a complete agent env", configured.Status)
	}

	incomplete := agentEnv()
	incomplete.ID = uuid.New()
	incomplete.Namespace = ""
	if err := svc.Create(incomplete); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if incomplete.Status != models.EnvStatusUnconfigured {
		t.Errorf("status = %q, want unconfigured when the namespace is missing", incomplete.Status)
	}
}

// cloneEnv copies an environment so a test can vary exactly one field while
// keeping the identity fields (id / target / namespace) the key-field
// comparison depends on. Building the "next" env with agentEnv() instead
// would randomize TargetID and make *every* update look like a key change —
// which both false-failed one case and false-passed another.
func cloneEnv(e *models.Environment) *models.Environment {
	c := *e
	return &c
}

func TestUpdate_KeyFieldChangeDowngradesAndClearsTestResult(t *testing.T) {
	cases := map[string]func(*models.Environment){
		"target":       func(e *models.Environment) { e.TargetID = uuid.New() },
		"namespace":    func(e *models.Environment) { e.Namespace = "other-ns" },
		"access":       func(e *models.Environment) { e.Access = models.EnvAccessSSH },
		"kubeCredRef":  func(e *models.Environment) { e.AccessConfig.KubeCredRef = "cred-1" },
		"sshSecretRef": func(e *models.Environment) { e.AccessConfig.SSHSecretRef = "cred-2" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			stored := agentEnv()
			lastTest := time.Now().Add(-time.Hour)
			stored.LastTestAt = &lastTest
			stored.LastTestResult = `{"status":"verified"}`
			store := newFakeEnvStore(stored)
			svc := NewEnvironmentService(store, &fakeTargetLookup{}, nil)

			next := cloneEnv(stored)
			mutate(next)

			if err := svc.Update(stored.ID, next); err != nil {
				t.Fatalf("Update: %v", err)
			}
			if next.Status != models.EnvStatusConfiguredUnverified {
				t.Errorf("status = %q, want configured_unverified after a key-field change", next.Status)
			}
			if next.LastTestAt != nil {
				t.Error("lastTestAt must be cleared — the previous verification no longer describes this config")
			}
			if next.LastTestResult != "" {
				t.Errorf("lastTestResult = %q, want it cleared", next.LastTestResult)
			}
		})
	}
}

func TestUpdate_NonKeyChangeKeepsVerification(t *testing.T) {
	stored := agentEnv()
	lastTest := time.Now().Add(-time.Hour)
	stored.LastTestAt = &lastTest
	stored.LastTestResult = `{"status":"verified"}`
	store := newFakeEnvStore(stored)
	svc := NewEnvironmentService(store, &fakeTargetLookup{}, nil)

	next := cloneEnv(stored)
	next.Name = "renamed" // display-only

	if err := svc.Update(stored.ID, next); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if next.Status != models.EnvStatusVerified {
		t.Errorf("status = %q, want the stored verified to survive a rename", next.Status)
	}
	if next.LastTestAt == nil || next.LastTestResult == "" {
		t.Error("a display-only change must not invalidate the last test result")
	}
}

func TestUpdate_UnknownPreviousDoesNotPanic(t *testing.T) {
	// Deleting and re-creating between two saves must not crash the service;
	// there is simply nothing to compare against.
	store := newFakeEnvStore()
	svc := NewEnvironmentService(store, &fakeTargetLookup{}, nil)

	next := agentEnv()
	next.Status = models.EnvStatusVerified
	if err := svc.Update(uuid.New(), next); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if next.Status != models.EnvStatusVerified {
		t.Errorf("status = %q, want it left alone when the old row is gone", next.Status)
	}
}

// --- connection checklist (§7.12.5) ---------------------------------------

func TestTest_PersistsVerifiedStatusOnAgentWithFreshTarget(t *testing.T) {
	env := agentEnv()
	env.Status = models.EnvStatusUnconfigured
	fresh := time.Now().Add(-time.Minute)
	store := newFakeEnvStore(env)
	lookup := &fakeTargetLookup{target: &targetmodels.Target{
		Status:          targetmodels.TargetStatusOnline,
		LastHeartbeatAt: &fresh,
	}}
	svc := NewEnvironmentService(store, lookup, nil)

	report, err := svc.Test(env.ID)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	if report.Status != models.EnvStatusVerified {
		t.Errorf("report status = %q, want verified; items=%+v", report.Status, report.Items)
	}
	if report.Passed != 3 || report.Total != 3 {
		t.Errorf("passed/total = %d/%d, want 3/3 (skipped connectivity items are excluded from total)",
			report.Passed, report.Total)
	}
	if report.TestedAt == "" {
		t.Error("testedAt must be filled in on the returned report")
	}
	if len(store.updated) != 1 {
		t.Fatalf("updated %d rows, want the env persisted once", len(store.updated))
	}
	if store.updated[0].Status != models.EnvStatusVerified {
		t.Errorf("persisted status = %q, want verified", store.updated[0].Status)
	}
	if store.updated[0].LastTestAt == nil {
		t.Error("persisted lastTestAt must be set")
	}
	if !strings.Contains(store.updated[0].LastTestResult, `"items"`) {
		t.Errorf("persisted lastTestResult = %q, want the serialized report", store.updated[0].LastTestResult)
	}
	// The archived snapshot must carry the verdict too, not just the item list:
	// report.Status used to be left empty, so both the API response and
	// last_test_result rendered as a blank status in the console.
	if !strings.Contains(store.updated[0].LastTestResult, `"status":"verified"`) {
		t.Errorf("persisted lastTestResult = %q, want it to record the verdict", store.updated[0].LastTestResult)
	}
}

func TestTest_MarksFailedWhenTargetIsOffline(t *testing.T) {
	env := agentEnv()
	store := newFakeEnvStore(env)
	lookup := &fakeTargetLookup{target: &targetmodels.Target{Status: targetmodels.TargetStatusOffline}}
	svc := NewEnvironmentService(store, lookup, nil)

	report, err := svc.Test(env.ID)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	if report.Status != models.EnvStatusFailed {
		t.Errorf("report status = %q, want failed for an offline target", report.Status)
	}
	if report.Passed == report.Total {
		t.Error("an offline target must produce at least one failing item")
	}
}

func TestTest_StaleHeartbeatCountsAsOffline(t *testing.T) {
	env := agentEnv()
	store := newFakeEnvStore(env)
	stale := time.Now().Add(-30 * time.Minute)
	lookup := &fakeTargetLookup{target: &targetmodels.Target{
		Status:          targetmodels.TargetStatusOnline,
		LastHeartbeatAt: &stale,
	}}
	svc := NewEnvironmentService(store, lookup, nil)

	report, err := svc.Test(env.ID)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	if report.Status != models.EnvStatusFailed {
		t.Errorf("report status = %q, want failed — 'online' with a 30-minute-old heartbeat is not online", report.Status)
	}
}

func TestTest_TargetLookupFailureIsToleratedAsFailureNot500(t *testing.T) {
	// The target row may be gone; the checklist should report that as a failed
	// item, not blow the whole request up.
	env := agentEnv()
	store := newFakeEnvStore(env)
	svc := NewEnvironmentService(store, &fakeTargetLookup{err: common.ErrResourceNotFound}, nil)

	report, err := svc.Test(env.ID)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	if report.Status != models.EnvStatusFailed {
		t.Errorf("report status = %q, want failed", report.Status)
	}
}

func TestTest_MissingEnvironmentReturnsError(t *testing.T) {
	svc := NewEnvironmentService(newFakeEnvStore(), &fakeTargetLookup{}, nil)

	if _, err := svc.Test(uuid.New()); err == nil {
		t.Fatal("testing a non-existent environment must return an error")
	}
}

// --- pure helpers ---------------------------------------------------------

func TestDeriveStatus(t *testing.T) {
	cases := []struct {
		name string
		env  models.Environment
		want string
	}{
		{"agent complete", models.Environment{Access: models.EnvAccessAgent, TargetID: uuid.New(), Namespace: "ns"}, models.EnvStatusConfiguredUnverified},
		{"agent without namespace", models.Environment{Access: models.EnvAccessAgent, TargetID: uuid.New()}, models.EnvStatusUnconfigured},
		{"agent with nil target", models.Environment{Access: models.EnvAccessAgent, Namespace: "ns"}, models.EnvStatusUnconfigured},
		{"kubeconfig with cred", models.Environment{Access: models.EnvAccessKubeconfig, AccessConfig: models.EnvAccessConfig{KubeCredRef: "c"}}, models.EnvStatusConfiguredUnverified},
		{"kubeconfig without cred", models.Environment{Access: models.EnvAccessKubeconfig}, models.EnvStatusUnconfigured},
		{"ssh complete", models.Environment{Access: models.EnvAccessSSH, AccessConfig: models.EnvAccessConfig{SSHSecretRef: "s", SSHTargets: []models.SSHEntry{{Host: "h", User: "u"}}}}, models.EnvStatusConfiguredUnverified},
		{"ssh without hosts", models.Environment{Access: models.EnvAccessSSH, AccessConfig: models.EnvAccessConfig{SSHSecretRef: "s"}}, models.EnvStatusUnconfigured},
		{"unknown access", models.Environment{Access: "carrier-pigeon"}, models.EnvStatusUnconfigured},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := deriveStatus(&tc.env); got != tc.want {
				t.Errorf("deriveStatus = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestKeyFieldChangedIgnoresDisplayFields(t *testing.T) {
	base := agentEnv()
	same := *base
	same.Name = "another name"
	same.Status = models.EnvStatusUnconfigured
	same.GroupID = nil
	if keyFieldChanged(base, &same) {
		t.Error("name / status / group are not key fields — renaming must not invalidate a verification")
	}

	changed := *base
	changed.Namespace = base.Namespace + "-v2"
	if !keyFieldChanged(base, &changed) {
		t.Error("a namespace change is a key change")
	}
}
