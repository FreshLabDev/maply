// SPDX-License-Identifier: Apache-2.0
//go:build integration

package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/FreshLabDev/maply/internal/free"
)

// A party shows its players as every table does: a name and a face. Who hid
// themselves keeps both there, as they chose to play together; no id leaves.
func TestPartyPlayersHaveFaces(t *testing.T) {
	hs := freeSetup(t)
	hs.srv.Avatars = &photosAsked{}
	hs.person(1, "Oksana")
	hs.person(7, "Vera")
	hs.expect("POST", "/api/profile/settings", initData(7, "Vera", "en", ""), `{"hide_me":true}`, http.StatusOK)
	oksana := initData(1, "Oksana", "en", "")
	lobby := decode[free.PartyView](t, hs.expect("POST", "/api/party", oksana, `{"mode":"classic","seconds":30,"rounds":3}`, 200))
	body := hs.expect("POST", "/api/party/"+lobby.Code+"/join", initData(7, "Vera", "en", "p"+lobby.Code), "", 200)
	v := decode[free.PartyView](t, body)
	if len(v.Members) != 2 || v.Members[0].Name != "Oksana" || v.Members[0].Photo != "/face/"+faceKey(1)+".jpg" ||
		v.Members[1].Name != "Vera" || !v.Members[1].Me || v.Members[1].Photo != "/face/"+faceKey(7)+".jpg" || v.Members[1].Anon {
		t.Fatalf("members: %+v", v.Members)
	}
	if strings.Contains(body, `"id"`) || strings.Contains(body, `"who"`) {
		t.Fatalf("the party carries an id or a handle: %s", body)
	}
	// The poll the same.
	poll := decode[free.PartyView](t, hs.expect("GET", "/api/party/"+lobby.Code, oksana, "", 200))
	if len(poll.Members) != 2 || poll.Members[1].Photo == "" {
		t.Fatalf("polled members: %+v", poll.Members)
	}
}

// A face is served at its address for good, and nothing at an address that
// is not one.
func TestFaceAddress(t *testing.T) {
	hs := setup(t)
	hs.srv.Avatars = &photosAsked{}
	get := func(path string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		hs.h.ServeHTTP(rr, httptest.NewRequest("GET", path, nil))
		return rr
	}
	rr := get("/face/" + faceKey(0xab) + ".jpg")
	if rr.Code != http.StatusOK || rr.Header().Get("Content-Type") != "image/jpeg" ||
		rr.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" || !bytes.Equal(rr.Body.Bytes(), []byte{0xff, 0xd8, 0xab}) {
		t.Fatalf("face: %d %v %v", rr.Code, rr.Header(), rr.Body.Bytes())
	}
	for _, bad := range []string{"/face/" + faceKey(0xab), "/face/" + strings.ToUpper(faceKey(0xab)) + ".jpg", "/face/..%2f.jpg", "/face/" + faceKey(0xab) + ".png"} {
		if rr := get(bad); rr.Code != http.StatusNotFound {
			t.Fatalf("%s: %d", bad, rr.Code)
		}
	}
}
