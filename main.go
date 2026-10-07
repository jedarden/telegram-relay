// Command telegram-relay is a small internal HTTP endpoint that forwards
// {chat_id, text} requests to the Telegram Bot API, injecting the bot token
// server-side so callers never need their own copy of it.
package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const maxRequestBytes = 64 * 1024

type sendRequest struct {
	ChatID    string `json:"chat_id"`
	Text      string `json:"text"`
	ParseMode string `json:"parse_mode"`
}

type relay struct {
	botToken    string
	defaultChat string
	authToken   string
	httpClient  *http.Client
}

func main() {
	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	if botToken == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN is required")
	}

	r := &relay{
		botToken:    botToken,
		defaultChat: os.Getenv("TELEGRAM_DEFAULT_CHAT_ID"),
		authToken:   os.Getenv("RELAY_AUTH_TOKEN"),
		httpClient:  &http.Client{Timeout: 10 * time.Second},
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handleHealthz)
	mux.HandleFunc("/send", r.handleSend)
	mux.HandleFunc("/alertmanager", r.handleAlertmanager)

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           logRequests(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("telegram-relay listening on :%s (default chat set: %v, auth required: %v)",
		port, r.defaultChat != "", r.authToken != "")
	log.Fatal(srv.ListenAndServe())
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (r *relay) handleSend(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !r.authorized(req) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	req.Body = http.MaxBytesReader(w, req.Body, maxRequestBytes)
	var body sendRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.Text) == "" {
		http.Error(w, "text is required", http.StatusBadRequest)
		return
	}
	chatID := body.ChatID
	if chatID == "" {
		chatID = r.defaultChat
	}
	if chatID == "" {
		http.Error(w, "chat_id is required (no TELEGRAM_DEFAULT_CHAT_ID configured)", http.StatusBadRequest)
		return
	}

	status, respBody, err := r.sendMessage(req.Context(), chatID, body.Text, body.ParseMode)
	if err != nil {
		http.Error(w, "telegram request failed: "+err.Error(), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(respBody)
}

func (r *relay) authorized(req *http.Request) bool {
	if r.authToken == "" {
		return true
	}
	want := "Bearer " + r.authToken
	got := req.Header.Get("Authorization")
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func (r *relay) sendMessage(ctx context.Context, chatID, text, parseMode string) (int, []byte, error) {
	payload := map[string]string{"chat_id": chatID, "text": text}
	if parseMode != "" {
		payload["parse_mode"] = parseMode
	}
	buf, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, err
	}

	upstream := "https://api.telegram.org/bot" + r.botToken + "/sendMessage"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, upstream, strings.NewReader(string(buf)))
	if err != nil {
		return 0, nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := r.httpClient.Do(httpReq)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxRequestBytes))
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, respBody, nil
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, req)
		log.Printf("%s %s -> %d (%s)", req.Method, req.URL.Path, sw.status, time.Since(start))
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

// telegramMaxText is Telegram's sendMessage limit, in characters.
const telegramMaxText = 4096

type amAlert struct {
	Status      string            `json:"status"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
}

type amPayload struct {
	Status      string            `json:"status"`
	Alerts      []amAlert         `json:"alerts"`
	CommonLabel map[string]string `json:"commonLabels"`
}

// handleAlertmanager accepts an Alertmanager webhook (payload version 4) and
// forwards a plain-text summary to the default chat, so an Alertmanager
// webhook_configs receiver can point straight at the relay. An optional
// ?chat_id= query parameter overrides the default chat.
func (r *relay) handleAlertmanager(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !r.authorized(req) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	req.Body = http.MaxBytesReader(w, req.Body, 1024*1024)
	var body amPayload
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		http.Error(w, "invalid Alertmanager payload: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(body.Alerts) == 0 {
		http.Error(w, "payload has no alerts", http.StatusBadRequest)
		return
	}
	chatID := req.URL.Query().Get("chat_id")
	if chatID == "" {
		chatID = r.defaultChat
	}
	if chatID == "" {
		http.Error(w, "chat_id is required (no TELEGRAM_DEFAULT_CHAT_ID configured)", http.StatusBadRequest)
		return
	}

	status, respBody, err := r.sendMessage(req.Context(), chatID, formatAlertmanager(body), "")
	if err != nil {
		http.Error(w, "telegram request failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(respBody)
}

func formatAlertmanager(p amPayload) string {
	var b strings.Builder
	firing, resolved := 0, 0
	for _, a := range p.Alerts {
		if a.Status == "resolved" {
			resolved++
		} else {
			firing++
		}
	}
	switch {
	case firing > 0 && resolved > 0:
		b.WriteString("ALERT " + itoa(firing) + " firing, " + itoa(resolved) + " resolved")
	case firing > 0:
		b.WriteString("FIRING (" + itoa(firing) + ")")
	default:
		b.WriteString("RESOLVED (" + itoa(resolved) + ")")
	}
	if c := p.CommonLabel["cluster"]; c != "" {
		b.WriteString(" [" + c + "]")
	}
	for _, a := range p.Alerts {
		b.WriteString("\n\n")
		if a.Status == "resolved" {
			b.WriteString("[resolved] ")
		}
		name := a.Labels["alertname"]
		if name == "" {
			name = "(unnamed alert)"
		}
		if sev := a.Labels["severity"]; sev != "" {
			b.WriteString(sev + ": ")
		}
		b.WriteString(name)
		if msg := firstNonEmpty(a.Annotations["summary"], a.Annotations["description"], a.Annotations["message"]); msg != "" {
			b.WriteString("\n" + msg)
		}
		var keys []string
		for k := range a.Labels {
			if k != "alertname" && k != "severity" && k != "cluster" {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		if len(keys) > 0 {
			parts := make([]string, 0, len(keys))
			for _, k := range keys {
				parts = append(parts, k+"="+a.Labels[k])
			}
			b.WriteString("\n" + strings.Join(parts, " "))
		}
	}
	out := b.String()
	if r := []rune(out); len(r) > telegramMaxText {
		out = string(r[:telegramMaxText-1]) + "…"
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func itoa(n int) string { return strconv.Itoa(n) }
