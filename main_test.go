package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFormatAlertmanager(t *testing.T) {
	p := amPayload{
		CommonLabel: map[string]string{"cluster": "iad-kalshi"},
		Alerts: []amAlert{
			{Status: "firing", Labels: map[string]string{"alertname": "ArmorRestoreStale", "severity": "page", "bucket": "b1"},
				Annotations: map[string]string{"summary": "no restore in 26h"}},
			{Status: "resolved", Labels: map[string]string{"alertname": "Other"}},
		},
	}
	got := formatAlertmanager(p)
	for _, want := range []string{"ALERT 1 firing, 1 resolved [iad-kalshi]", "page: ArmorRestoreStale", "no restore in 26h", "bucket=b1", "[resolved] Other"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestFormatAlertmanagerTruncates(t *testing.T) {
	p := amPayload{}
	for i := 0; i < 200; i++ {
		p.Alerts = append(p.Alerts, amAlert{Status: "firing", Labels: map[string]string{"alertname": strings.Repeat("x", 60)},
			Annotations: map[string]string{"summary": strings.Repeat("y", 60)}})
	}
	if n := len([]rune(formatAlertmanager(p))); n > telegramMaxText {
		t.Fatalf("message %d chars exceeds limit", n)
	}
}

func TestHandleAlertmanagerValidation(t *testing.T) {
	r := &relay{authToken: "tok", defaultChat: "1", httpClient: http.DefaultClient}
	cases := []struct {
		name, method, auth, body string
		want                     int
	}{
		{"wrong method", "GET", "Bearer tok", "", http.StatusMethodNotAllowed},
		{"unauthorized", "POST", "", `{}`, http.StatusUnauthorized},
		{"bad json", "POST", "Bearer tok", `{`, http.StatusBadRequest},
		{"no alerts", "POST", "Bearer tok", `{"alerts":[]}`, http.StatusBadRequest},
	}
	for _, c := range cases {
		req := httptest.NewRequest(c.method, "/alertmanager", strings.NewReader(c.body))
		if c.auth != "" {
			req.Header.Set("Authorization", c.auth)
		}
		w := httptest.NewRecorder()
		r.handleAlertmanager(w, req)
		if w.Code != c.want {
			t.Errorf("%s: got %d want %d", c.name, w.Code, c.want)
		}
	}
	_ = json.Valid
}
