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
	// Many in one read: the ones with a photo, nobody else.
	if err := store.SetAvatar(ctx, 8, Avatar{CheckedAt: at}); err != nil {
		t.Fatal(err)
	}
	m, err := store.AvatarsOf(ctx, []int64{7, 8, 9})
	if err != nil || len(m) != 1 || !bytes.Equal(m[7], []byte{1, 2, 3}) {
		t.Fatalf("AvatarsOf = %v, %v", m, err)
	}
}

// A kept photo has an address of its own: the same photo keeps it, a new
// photo gets a new one, and no photo has none.
func TestAvatarFaceAddress(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	at := time.Date(2026, 11, 6, 18, 0, 0, 0, time.UTC)
	if err := store.SetAvatar(ctx, 7, Avatar{UniqueID: "u1", JPEG: []byte{1}, CheckedAt: at}); err != nil {
		t.Fatal(err)
	}
	a, _, _ := store.AvatarOf(ctx, 7)
	if len(a.Face) != 32 {
		t.Fatalf("face %q", a.Face)
	}
	if b, ok, err := store.FaceJPEG(ctx, a.Face); err != nil || !ok || !bytes.Equal(b, []byte{1}) {
		t.Fatalf("FaceJPEG = %v %v %v", b, ok, err)
	}
	// Asked again, the same photo: the same address.
	if err := store.SetAvatar(ctx, 7, Avatar{UniqueID: "u1", JPEG: []byte{1}, Face: a.Face, CheckedAt: at.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if again, _, _ := store.AvatarOf(ctx, 7); again.Face != a.Face {
		t.Fatalf("same photo, face %q then %q", a.Face, again.Face)
	}
	// A new photo: a new address, and the old one shows nothing.
	if err := store.SetAvatar(ctx, 7, Avatar{UniqueID: "u2", JPEG: []byte{2}, CheckedAt: at.Add(2 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	b, _, _ := store.AvatarOf(ctx, 7)
	if b.Face == "" || b.Face == a.Face {
		t.Fatalf("new photo, face %q then %q", a.Face, b.Face)
	}
	if _, ok, err := store.FaceJPEG(ctx, a.Face); err != nil || ok {
		t.Fatalf("old address: ok %v, %v", ok, err)
	}
	faces, err := store.FacesOf(ctx, []int64{7, 8})
	if err != nil || len(faces) != 1 || faces[7] != b.Face {
		t.Fatalf("FacesOf = %v, %v", faces, err)
	}
	// No photo any more: no address.
	if err := store.SetAvatar(ctx, 7, Avatar{CheckedAt: at.Add(3 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if c, _, _ := store.AvatarOf(ctx, 7); c.Face != "" {
		t.Fatalf("no photo, face %q", c.Face)
	}
	if faces, err := store.FacesOf(ctx, []int64{7}); err != nil || len(faces) != 0 {
		t.Fatalf("FacesOf without a photo = %v, %v", faces, err)
	}
}
