// Copyright 2026 cymx333 and contributors.
// SPDX-License-Identifier: Apache-2.0
package chatgptdirect

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStreamCompletion(t *testing.T) {
	for _, tc := range []struct {
		name, stream string
		ok           bool
	}{
		{"completed", "data: {\"type\":\"response.output_text.delta\",\"delta\":\"你好\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"model\":\"gpt-6-luna\"}}\n", true},
		{"partial", "data: {\"type\":\"response.output_text.delta\",\"delta\":\"不能交付\"}\n", false},
		{"late failure", "data: {\"type\":\"response.output_text.delta\",\"delta\":\"不能交付\"}\n\ndata: {\"type\":\"response.failed\"}\n", false},
		{"wrong model", "data: {\"type\":\"response.output_text.delta\",\"delta\":\"不能交付\"}\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"model\":\"other\"}}\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text, err := ReadResponse(strings.NewReader(tc.stream))
			if (err == nil) != tc.ok {
				t.Fatalf("unexpected success=%v", err == nil)
			}
			if !tc.ok && text != "" {
				t.Fatal("partial answer leaked")
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestRefreshPersistsRotatingToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "account.json")
	before := `{"issuer":"https://auth.openai.com","client_id":"oaiapp_test","subject":"fixture","access_token":"old-access","refresh_token":"old-refresh","saved_at":0,"expires_in":1,"scopes":["chatgpt.tokens.use.direct"]}`
	if err := os.WriteFile(path, []byte(before), 0600); err != nil {
		t.Fatal(err)
	}
	c := Client{Path: path, HTTP: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://auth.openai.com/api/accounts/oauth/token" || r.Method != "POST" {
			t.Fatal("unexpected destination")
		}
		r.ParseForm()
		if r.Form.Get("refresh_token") != "old-refresh" || r.Form.Get("client_id") != "oaiapp_test" {
			t.Fatal("wrong refresh identity")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"access_token":"new-access","refresh_token":"new-refresh","token_type":"Bearer","expires_in":3600,"scope":"chatgpt.tokens.use.direct resource.invoke"}`)), Header: make(http.Header)}, nil
	})}}
	token, err := c.token(context.Background())
	if err != nil || token != "new-access" {
		t.Fatalf("refresh failed: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var d map[string]any
	json.Unmarshal(b, &d)
	if d["refresh_token"] != "new-refresh" || d["subject"] != "fixture" {
		t.Fatal("rotated token or identity not preserved")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("credential mode is not private")
	}
}
