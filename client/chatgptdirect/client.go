// Copyright 2026 cymx333 and contributors.
// SPDX-License-Identifier: Apache-2.0
// Package chatgptdirect uses the official, independently authorized ChatGPT
// plan Responses API flow. It never reads Codex's account store or API keys.
package chatgptdirect

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const Model = "gpt-6-luna"
const Resource = "https://api.openai.com/v1"

type Client struct {
	Path string
	HTTP *http.Client
	mu   sync.Mutex
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 25 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
func hasPermission(v any) bool {
	a, ok := v.([]any)
	if !ok {
		return false
	}
	for _, s := range a {
		if s == "chatgpt.tokens.use.direct" {
			return true
		}
	}
	return false
}
func (c *Client) token(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	raw, err := os.ReadFile(c.Path)
	if err != nil {
		return "", errors.New("cannot read authorized account")
	}
	var d map[string]any
	if json.Unmarshal(raw, &d) != nil || d["issuer"] != "https://auth.openai.com" || !hasPermission(d["scopes"]) {
		return "", errors.New("invalid account authorization")
	}
	clientID, _ := d["client_id"].(string)
	subject, _ := d["subject"].(string)
	token, _ := d["access_token"].(string)
	if !strings.HasPrefix(clientID, "oaiapp_") || subject == "" || token == "" {
		return "", errors.New("incomplete account authorization")
	}
	saved, _ := d["saved_at"].(float64)
	expires, _ := d["expires_in"].(float64)
	if float64(time.Now().Unix()) < saved+expires-90 {
		return token, nil
	}
	refresh, _ := d["refresh_token"].(string)
	if refresh == "" {
		return "", errors.New("sign-in required")
	}
	form := url.Values{"grant_type": {"refresh_token"}, "client_id": {clientID}, "refresh_token": {refresh}, "resource": {Resource}}
	req, _ := http.NewRequestWithContext(ctx, "POST", "https://auth.openai.com/api/accounts/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http().Do(req)
	if err != nil {
		return "", errors.New("account refresh connection failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("account refresh HTTP %d; sign-in may be required", resp.StatusCode)
	}
	var updated map[string]any
	if json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&updated) != nil {
		return "", errors.New("invalid refresh response")
	}
	next, _ := updated["access_token"].(string)
	kind, _ := updated["token_type"].(string)
	ttl, _ := updated["expires_in"].(float64)
	if next == "" || !strings.EqualFold(kind, "bearer") || ttl <= 0 {
		return "", errors.New("incomplete refresh response")
	}
	if s, ok := updated["scope"].(string); ok {
		scopes := []any{}
		for _, v := range strings.Fields(s) {
			scopes = append(scopes, v)
		}
		if !hasPermission(scopes) {
			return "", errors.New("ChatGPT plan permission missing")
		}
		d["scopes"] = scopes
	}
	d["access_token"] = next
	d["expires_in"] = ttl
	d["saved_at"] = float64(time.Now().Unix())
	if r, ok := updated["refresh_token"].(string); ok && r != "" {
		d["refresh_token"] = r
	}
	// Retain the previously validated ID token as a reauthorization hint.
	data, _ := json.Marshal(d)
	tmp, err := os.CreateTemp(filepath.Dir(c.Path), ".account-*")
	if err != nil {
		return "", errors.New("cannot persist refreshed account")
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err = tmp.Chmod(0600); err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(name, c.Path)
	}
	if err != nil {
		return "", errors.New("cannot persist refreshed account")
	}
	return next, nil
}
func (c *Client) Available(ctx context.Context) error {
	token, err := c.token(ctx)
	if err != nil {
		return err
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", Resource+"/models", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.http().Do(req)
	if err != nil {
		return errors.New("model catalog connection failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("model catalog HTTP %d", resp.StatusCode)
	}
	var catalog struct {
		Models []struct {
			Slug string `json:"slug"`
		} `json:"models"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&catalog) != nil {
		return errors.New("invalid model catalog")
	}
	for _, m := range catalog.Models {
		if m.Slug == Model {
			return nil
		}
	}
	return errors.New("requested GPT-6 Luna is unavailable; no fallback used")
}
func (c *Client) Answer(ctx context.Context, text string) (string, error) {
	if strings.TrimSpace(text) == "" || len([]rune(text)) > 2000 {
		return "", errors.New("invalid query")
	}
	token, err := c.token(ctx)
	if err != nil {
		return "", err
	}
	body, _ := json.Marshal(map[string]any{"model": Model, "store": false, "stream": true, "reasoning": map[string]string{"effort": "none"}, "instructions": "你是小爱音箱的中文语音问答助手。直接回答，通常不超过100字，不使用Markdown。没有联网搜索，不能编造实时信息。不要调用工具或执行设备操作。", "input": []map[string]string{{"role": "user", "content": text}}})
	req, _ := http.NewRequestWithContext(ctx, "POST", Resource+"/responses", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http().Do(req)
	if err != nil {
		return "", errors.New("inference connection failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("inference HTTP %d", resp.StatusCode)
	}
	return ReadResponse(resp.Body)
}

// A partial stream is never treated as an answer. Late quota/errors invalidate it.
func ReadResponse(r io.Reader) (string, error) {
	scanner := bufio.NewScanner(io.LimitReader(r, 4<<20))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	var answer strings.Builder
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var event struct {
			Type     string `json:"type"`
			Delta    string `json:"delta"`
			Response struct {
				Status string `json:"status"`
				Model  string `json:"model"`
			} `json:"response"`
		}
		if json.Unmarshal([]byte(data), &event) != nil {
			return "", errors.New("invalid response event")
		}
		switch event.Type {
		case "response.output_text.delta":
			answer.WriteString(event.Delta)
		case "response.failed", "response.incomplete", "error":
			return "", errors.New("inference did not complete")
		case "response.completed":
			if event.Response.Status != "completed" || event.Response.Model != Model {
				return "", errors.New("unexpected completed model or status")
			}
			text := strings.TrimSpace(answer.String())
			if text == "" {
				return "", errors.New("empty answer")
			}
			return text, nil
		}
	}
	return "", errors.New("stream ended before completed inference")
}
