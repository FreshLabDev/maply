// SPDX-License-Identifier: Apache-2.0
//go:build integration

package db

import (
	"bytes"
	"context"
	"testing"
	"time"
)

func TestAvatarKeepsAPhotoOrNone(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	if _, ok, err := store.AvatarOf(ctx, 7); err != nil || ok {
		t.Fatalf("never asked: ok %v, %v", ok, err)
	}
	at := time.Date(2026, 10, 26, 18, 0, 0, 0, time.UTC)
	if err := store.SetAvatar(ctx, 7, Avatar{CheckedAt: at}); err != nil {
		t.Fatal(err)
	}
	a, ok, err := store.AvatarOf(ctx, 7)
	if err != nil || !ok || a.JPEG != nil || a.UniqueID != "" || !a.CheckedAt.Equal(at) {
		t.Fatalf("no photo: %+v %v %v", a, ok, err)
	}
	if err := store.SetAvatar(ctx, 7, Avatar{UniqueID: "u1", JPEG: []byte{1, 2, 3}, CheckedAt: at.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if a, _, _ := store.AvatarOf(ctx, 7); a.UniqueID != "u1" || !bytes.Equal(a.JPEG, []byte{1, 2, 3}) {
		t.Fatalf("photo: %+v", a)
	}
}
