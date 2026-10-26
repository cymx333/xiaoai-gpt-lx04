// Copyright 2026 cymx333 and contributors.
// SPDX-License-Identifier: Apache-2.0
// Connectivity-only probe. No credentials and no inference requests.
package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"runtime"
	"sync"
	"time"
	"xiaobot/runtimeenv"
)

func main() {
	if err := runtimeenv.ConfigureDNSFromEnv(); err != nil {
		panic(err)
	}
	json.NewEncoder(os.Stdout).Encode(map[string]any{"arch": runtime.GOARCH, "go": runtime.Version()})
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, host := range []string{"api.openai.com", "auth.openai.com", "api2.mina.mi.com"} {
		wg.Add(1)
		go func(host string) {
			defer wg.Done()
			start := time.Now()
			row := map[string]any{"host": host}
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			ips, err := net.DefaultResolver.LookupHost(ctx, host)
			row["dns_ms"] = time.Since(start).Milliseconds()
			if err != nil {
				row["error"] = err.Error()
			} else {
				row["addresses"] = ips
				path := "/"
				if host == "api.openai.com" {
					path = "/v1/models"
				}
				req, _ := http.NewRequestWithContext(ctx, "GET", "https://"+host+path, nil)
				client := http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
				resp, e := client.Do(req)
				if e != nil {
					row["error"] = e.Error()
				} else {
					row["http_status"] = resp.StatusCode
					resp.Body.Close()
				}
			}
			row["total_ms"] = time.Since(start).Milliseconds()
			mu.Lock()
			json.NewEncoder(os.Stdout).Encode(row)
			mu.Unlock()
		}(host)
	}
	wg.Wait()
}
