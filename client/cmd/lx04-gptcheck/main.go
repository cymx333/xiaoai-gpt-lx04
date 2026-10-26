// Copyright 2026 cymx333 and contributors.
// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"
	"xiaobot/chatgptdirect"
	"xiaobot/runtimeenv"
)

func main() {
	path := flag.String("account", "", "independently authorized ChatGPT account file")
	prompt := flag.String("prompt", "", "optional bounded test question")
	flag.Parse()
	if err := runtimeenv.ConfigureDNSFromEnv(); err != nil {
		fmt.Println("invalid DNS")
		os.Exit(1)
	}
	c := chatgptdirect.Client{Path: *path}
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	if err := c.Available(ctx); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	fmt.Println("account_model_available=gpt-6-luna")
	if *prompt != "" {
		started := time.Now()
		text, err := c.Answer(ctx, *prompt)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		fmt.Printf("completed=true seconds=%.2f answer=%s\n", time.Since(started).Seconds(), text)
	}
}
