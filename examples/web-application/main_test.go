package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBasicAuth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		user, password     string
		sendUser, sendPass string
		send               bool
		status             int
	}{
		{name: "unset_credentials_deny_empty_login", send: true, status: http.StatusUnauthorized},
		{name: "unset_credentials_deny_anonymous", status: http.StatusUnauthorized},
		{name: "unset_password_denies", user: "admin", sendUser: "admin", send: true, status: http.StatusUnauthorized},
		{name: "no_credentials", user: "admin", password: "s3cret", status: http.StatusUnauthorized},
		{name: "wrong_password", user: "admin", password: "s3cret", sendUser: "admin", sendPass: "wrong", send: true, status: http.StatusUnauthorized},
		{name: "wrong_user", user: "admin", password: "s3cret", sendUser: "root", sendPass: "s3cret", send: true, status: http.StatusUnauthorized},
		{name: "right_credentials", user: "admin", password: "s3cret", sendUser: "admin", sendPass: "s3cret", send: true, status: http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/debug/doger", http.NoBody)
			if tt.send {
				req.SetBasicAuth(tt.sendUser, tt.sendPass)
			}

			rec := httptest.NewRecorder()
			basicAuth(tt.user, tt.password)(next).ServeHTTP(rec, req)

			if rec.Code != tt.status {
				t.Errorf("status %d, want %d", rec.Code, tt.status)
			}
		})
	}
}
