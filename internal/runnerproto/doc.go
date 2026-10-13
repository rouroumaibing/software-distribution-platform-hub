// Package runnerproto is a hub-side replica of the hub↔runner wire-protocol
// types defined in the runner repository's api/v1alpha1 package.
//
// Source of truth:
//
//	github.com/rouroumaibing/software-distribution-platform-runner
//	api/v1alpha1, at commit cfe8619a0ec2f77b03214bb36a199f100c932b7e.
//
// This package exists only so the hub can build and package independently of
// the runner source tree (the runner repo is not a build-time dependency of
// the hub). It is a derived snapshot, NOT a second source of truth.
//
// Consistency rules (see shared/ADR-dispatch-durable-queue.md §10):
//   - runner api/v1alpha1 remains the single source of truth;
//   - when the wire protocol changes, update runner FIRST, then mirror the
//     change here (copy JSON tags and constant values verbatim, keep the
//     transitive closure complete);
//   - compatibility policy: add-only, never change an enum string value
//     (values are persisted in hub DB columns); breaking changes go through a
//     new versioned package.
//
// Do not add business logic here. Types are pure value containers for
// (de)serialization over the gateway connection.
package runnerproto
