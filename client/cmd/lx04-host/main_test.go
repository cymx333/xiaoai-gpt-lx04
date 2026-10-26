package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestModelWorkerHelper(t *testing.T) {
	if os.Getenv("LX04_TEST_HELPER") != "1" {
		return
	}
	fmt.Println(`{"ready":true}`)
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), "blocked") {
			time.Sleep(10 * time.Second)
		}
		fmt.Println(`{"text":"answer"}`)
	}
	os.Exit(0)
}
func helperModel(t *testing.T) *modelProcess {
	t.Helper()
	t.Setenv("LX04_TEST_HELPER", "1")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	m := &modelProcess{python: exe, script: "-test.run=^TestModelWorkerHelper$"}
	t.Cleanup(func() { m.mu.Lock(); defer m.mu.Unlock(); m.stop() })
	return m
}
func TestWarmupCancellationDoesNotKillReader(t *testing.T) {
	m := helperModel(t)
	warm, cancel := context.WithTimeout(context.Background(), time.Second)
	if err := m.warm(warm); err != nil {
		t.Fatal(err)
	}
	cancel()
	ctx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	for i := 0; i < 2; i++ {
		got, err := m.answer(ctx, "question")
		if err != nil || got != "answer" {
			t.Fatalf("got=%q err=%v", got, err)
		}
	}
}
func TestCancelledAnswerRestartsWorker(t *testing.T) {
	m := helperModel(t)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if _, err := m.answer(ctx, "blocked"); err == nil {
		t.Fatal("expected cancellation")
	}
	next, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if got, err := m.answer(next, "next"); err != nil || got != "answer" {
		t.Fatalf("got=%q err=%v", got, err)
	}
}
