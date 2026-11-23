package main

import (
	"testing"

	artifactstorage "github.com/rouroumaibing/software-distribution-platform-hub/internal/artifact/storage"
)

// A typed-nil driver pointer must not escape as a non-nil ObjectDeleter.
// Retention GC checks the interface for nil before calling Delete, so a
// wrapped nil pointer would pass the check and panic on the first call.
func TestObjectDeleterFromDriversRejectsTypedNil(t *testing.T) {
	var local *artifactstorage.Local
	var s3 *artifactstorage.S3

	if got := objectDeleterFromDrivers(local, s3); got != nil {
		t.Fatalf("both drivers nil: want nil deleter, got %T", got)
	}
	if got := objectDeleterFromDrivers(nil, s3); got != nil {
		t.Fatalf("typed-nil s3: want nil deleter, got %T", got)
	}

	realLocal := &artifactstorage.Local{}
	realS3 := &artifactstorage.S3{}
	if got := objectDeleterFromDrivers(realLocal, s3); got == nil {
		t.Fatal("local driver present: want non-nil deleter")
	}
	if got := objectDeleterFromDrivers(local, realS3); got == nil {
		t.Fatal("s3 driver present with typed-nil local: want non-nil deleter")
	}
	if got := objectDeleterFromDrivers(realLocal, realS3); got != realLocal {
		t.Fatal("local driver takes precedence")
	}
}
