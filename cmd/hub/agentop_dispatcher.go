package main

// agentOpDispatcher closes the loop between the agent_ops ledger (§9.5 exec /
// §9.9 接入编排) and the gateway: it assembles the wire payload for a queued
// op and pushes it down the target's live Runner connection.
//
// Assembly rules (UNIMPLEMENTED-MODULES-PLAN §16.5 steelman):
//   - Namespace comes from the op's environment, so the Runner's op Job lands
//     where the operator scoped the environment.
//   - Kubeconfig is attached only for kubeconfig-access environments: the
//     Runner is the direct-connect executor and legitimately needs the
//     cluster access the hub deliberately does not have (hub 无 client-go)。
//     The credential is decrypted here (codec) and shipped over the
//     authenticated gateway WS — the documented trust boundary.
//   - install / upgrade are NOT dispatched: their executors depend on the
//     §9.9 bootstrap flow (enroll-token credential handover to a target that
//     has no Runner yet), a separate feature. They stay queued by design —
//     console's「已受理待执行」semantics unchanged (docs/hub/API-REFERENCE.md).

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	runnerapi "github.com/rouroumaibing/software-distribution-platform-runner/api/v1alpha1"

	applog "github.com/rouroumaibing/software-distribution-platform-hub/internal/common/logger"
	credcodec "github.com/rouroumaibing/software-distribution-platform-hub/internal/credentials/codec"
	credrepo "github.com/rouroumaibing/software-distribution-platform-hub/internal/credentials/repository"
	envmodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/environment/models"
	envrepo "github.com/rouroumaibing/software-distribution-platform-hub/internal/environment/repository"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/gateway"
	targetmodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/target/models"
)

type agentOpDispatcher struct {
	gw       *gateway.HubServer
	envRepo  *envrepo.EnvironmentRepository
	credRepo *credrepo.CredentialRepository
}

// Dispatch satisfies targetsvc.OpDispatcher.
func (d *agentOpDispatcher) Dispatch(op *targetmodels.AgentOp) error {
	if op.OpType != targetmodels.AgentOpExec {
		// Bootstrap-dependent ops stay queued (§16.5 裁定); not an error.
		return nil
	}
	payload, err := d.buildPayload(op)
	if err != nil {
		// The op cannot ever be executed as-is (dangling env / credential);
		// log loudly and leave it queued rather than fabricating a Runner
		// report — the ledger row is the audit trail.
		applog.Warnf("agentop: payload assembly failed for op %s (%s): %v — op stays queued", op.ID, op.OpType, err)
		return nil
	}
	return d.gw.DispatchAgentOp(context.Background(), op.TargetID, payload)
}

func (d *agentOpDispatcher) buildPayload(op *targetmodels.AgentOp) (*runnerapi.AgentOpDispatchPayload, error) {
	p := &runnerapi.AgentOpDispatchPayload{
		OpID:     op.ID.String(),
		TargetID: op.TargetID.String(),
		OpType:   op.OpType,
		Detail:   op.Detail,
	}
	if op.EnvID == nil {
		return p, nil
	}
	p.EnvID = op.EnvID.String()
	env, err := d.envRepo.GetByID(*op.EnvID)
	if err != nil {
		return nil, fmt.Errorf("resolve environment: %w", err)
	}
	p.Namespace = env.Namespace
	if env.Access != envmodels.EnvAccessKubeconfig {
		// agent access: the Runner executes inside its own cluster with
		// in-cluster credentials — nothing to hand over.
		return p, nil
	}
	if env.AccessConfig.KubeCredRef == "" {
		return nil, fmt.Errorf("kubeconfig-access env %s has no KubeCredRef", env.ID)
	}
	credID, err := uuid.Parse(env.AccessConfig.KubeCredRef)
	if err != nil {
		return nil, fmt.Errorf("bad KubeCredRef %q: %w", env.AccessConfig.KubeCredRef, err)
	}
	cred, err := d.credRepo.GetByID(credID)
	if err != nil {
		return nil, fmt.Errorf("resolve credential %s: %w", credID, err)
	}
	plain, err := credcodec.Decrypt(cred.Value)
	if err != nil {
		return nil, fmt.Errorf("decrypt credential %s: %w", credID, err)
	}
	p.Kubeconfig = []byte(plain)
	return p, nil
}
