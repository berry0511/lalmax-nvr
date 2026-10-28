package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lalmax-pro/lalmax-nvr/internal/middleware"
	"github.com/lalmax-pro/lalmax-nvr/internal/model"
)

func TestDerivedPlaybackRoutesRequireAdministrator(t *testing.T) {
	db, store := setupTestDB(t)
	defer db.Close()
	viewer := TestHandler(db, store)
	viewer.SetMultiUserAuthMW(middleware.InjectUser(&model.User{Role: model.RoleUser, Enabled: true}))
	hash, err := middleware.HashPassword("test-password")
	if err != nil {
		t.Fatal(err)
	}
	anonymous := TestHandlerWithAuth(db, store, "admin", hash)
	paths := []string{
		"/api/streams/camera/variants",
		"/api/streams/camera/variants/job/play",
		"/api/streams/camera/variants/job/stream.flv",
	}
	for _, path := range paths {
		for _, tc := range []struct {
			name    string
			handler *Handler
			want    int
		}{
			{"viewer", viewer, http.StatusForbidden},
			{"anonymous", anonymous, http.StatusUnauthorized},
		} {
			t.Run(tc.name+path, func(t *testing.T) {
				w := httptest.NewRecorder()
				tc.handler.Routes().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
				if w.Code != tc.want {
					t.Fatalf("got %d, want %d", w.Code, tc.want)
				}
			})
		}
	}
}
