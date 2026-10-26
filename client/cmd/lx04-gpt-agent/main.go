// Copyright 2026 cymx333 and contributors.
// SPDX-License-Identifier: Apache-2.0
// Standalone trial: local speech gating, official ChatGPT OAuth, Xiaomi TTS.
// No firmware changes or startup installation are performed by this binary.
package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"xiaobot"
	"xiaobot/chatgptdirect"
	"xiaobot/devicevoice"
	"xiaobot/runtimeenv"
)

type localGate struct {
	dir   string
	audio *devicevoice.AudioGate
}

func (g *localGate) locked(fn func() error) error {
	f, err := os.OpenFile(filepath.Join(g.dir, "audio.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return fn()
}
func (g *localGate) restore() error {
	return g.locked(func() error {
		if err := g.audio.Restore(); err != nil {
			return err
		}
		os.Remove(filepath.Join(g.dir, "watchdog.nonce"))
		return nil
	})
}
func (g *localGate) mute() error {
	return g.locked(func() error {
		nonce := make([]byte, 16)
		if _, err := rand.Read(nonce); err != nil {
			return err
		}
		value := hex.EncodeToString(nonce)
		if err := os.WriteFile(filepath.Join(g.dir, "watchdog.nonce"), []byte(value), 0600); err != nil {
			return err
		}
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		cmd := exec.Command(exe, "-directory", g.dir, "-watchdog", value)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		pipe, err := cmd.StdoutPipe()
		if err != nil {
			return err
		}
		defer pipe.Close()
		if err = cmd.Start(); err != nil {
			return err
		}
		ready := make(chan bool, 1)
		go func() { s := bufio.NewScanner(pipe); ready <- s.Scan() && s.Text() == "armed" }()
		go cmd.Wait()
		select {
		case ok := <-ready:
			if !ok {
				return errors.New("watchdog did not arm")
			}
		case <-time.After(3 * time.Second):
			return errors.New("watchdog startup timeout")
		}
		return g.audio.Mute()
	})
}
func watchdog(g *localGate, nonce string) {
	fmt.Println("armed")
	os.Stdout.Close()
	time.Sleep(27 * time.Second)
	g.locked(func() error {
		b, err := os.ReadFile(filepath.Join(g.dir, "watchdog.nonce"))
		if err != nil || string(b) != nonce {
			return nil
		}
		err = g.audio.Restore()
		if err == nil {
			os.Remove(filepath.Join(g.dir, "watchdog.nonce"))
		}
		return err
	})
}
func parse(line string) devicevoice.Event {
	e := devicevoice.ParseEvent(line)
	if e.Kind != "" {
		return e
	}
	if strings.Contains(line, "MSG_ASR_PARTIAL_RESULT:") {
		return devicevoice.Event{Kind: "listening"}
	}
	i := strings.Index(line, "instruction=")
	if i < 0 {
		return e
	}
	var item struct {
		Header  struct{ Namespace, Name string }
		Payload struct {
			Final   bool              `json:"is_final"`
			Results []json.RawMessage `json:"results"`
		}
	}
	if json.Unmarshal([]byte(line[i+len("instruction="):]), &item) == nil && item.Header.Namespace == "SpeechRecognizer" && item.Header.Name == "RecognizeResult" && !item.Payload.Final && len(item.Payload.Results) > 0 {
		return devicevoice.Event{Kind: "listening"}
	}
	return e
}

type agent struct {
	mu            sync.Mutex
	ttsMu         sync.Mutex
	gate          *localGate
	model         *chatgptdirect.Client
	box           *xiaobot.XiaoMi
	ctx           context.Context
	cancel        context.CancelFunc
	generation    int
	listening     bool
	pending, last string
	lastAt        time.Time
}

func (a *agent) restore() {
	if a.gate.restore() != nil {
		log.Print("restore_failed; independent watchdog remains available")
	}
}
func (a *agent) event(e devicevoice.Event) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if e.Kind == "speak" {
		if a.pending != "" && strings.TrimSpace(e.Text) == a.pending {
			a.pending = ""
			a.restore()
			log.Print("gpt_speech_observed")
		}
		return
	}
	if e.Kind == "listening" {
		if a.listening {
			return
		}
		a.listening = true
		a.generation++
		gen := a.generation
		if a.cancel != nil {
			a.cancel()
		}
		a.pending = ""
		if a.gate.mute() != nil {
			a.restore()
			a.listening = false
			return
		}
		log.Print("gate=partial_asr")
		go func() {
			select {
			case <-time.After(8 * time.Second):
			case <-a.ctx.Done():
			}
			a.mu.Lock()
			defer a.mu.Unlock()
			if a.generation == gen && a.listening {
				a.listening = false
				a.restore()
			}
		}()
		return
	}
	if e.Kind != "query" {
		return
	}
	q := strings.TrimSpace(e.Text)
	if q == "" || len([]rune(q)) > 2000 {
		return
	}
	if q == a.last && time.Since(a.lastAt) < 2*time.Second {
		return
	}
	a.last = q
	a.lastAt = time.Now()
	a.listening = false
	a.generation++
	gen := a.generation
	if a.cancel != nil {
		a.cancel()
	}
	a.pending = ""
	if devicevoice.NativeRequest(q) {
		a.restore()
		log.Print("route=native")
		return
	}
	started := time.Now()
	if a.gate.mute() != nil {
		a.restore()
		log.Print("gate_failed")
		return
	}
	log.Printf("route=gpt-6-luna gate_ms=%d", time.Since(started).Milliseconds())
	ctx, cancel := context.WithTimeout(a.ctx, 23*time.Second)
	a.cancel = cancel
	go a.answer(ctx, gen, q)
	go func() {
		<-ctx.Done()
		a.mu.Lock()
		defer a.mu.Unlock()
		if a.generation == gen {
			a.pending = ""
			a.restore()
		}
	}()
}
func (a *agent) answer(ctx context.Context, gen int, q string) {
	start := time.Now()
	answer, err := a.model.Answer(ctx, q)
	if ctx.Err() != nil {
		return
	}
	if err != nil {
		log.Printf("model_failed: %v", err)
		answer = "这次 GPT 连接没有成功，请稍后再试。"
	} else {
		log.Printf("model=gpt-6-luna answer_seconds=%.2f", time.Since(start).Seconds())
	}
	answer = devicevoice.SpeechText(answer)
	a.ttsMu.Lock()
	defer a.ttsMu.Unlock()
	a.mu.Lock()
	if a.generation != gen || ctx.Err() != nil {
		a.mu.Unlock()
		return
	}
	a.pending = answer
	a.mu.Unlock()
	err = a.box.MiTTS(answer)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.generation != gen {
		return
	}
	if err != nil {
		a.pending = ""
		a.restore()
		log.Print("tts_failed")
		return
	}
	// Unmute only when this exact answer reaches the speaker. The former one-second
	// timer could expose the still-playing native response before GPT speech arrived.
	log.Print("tts_accepted; waiting_for_matching_speech")
}
func main() {
	dir := flag.String("directory", "", "private runtime directory")
	watch := flag.String("watchdog", "", "internal watchdog nonce")
	restore := flag.Bool("restore-audio", false, "restore original playback state")
	audioTest := flag.Bool("audio-test", false, "test local gate and recovery")
	watchdogTest := flag.Bool("watchdog-test", false, "verify independent recovery")
	selfTest := flag.Bool("self-test", false, "perform model and native speech test")
	flag.Parse()
	if *dir == "" {
		log.Fatal("directory required")
	}
	gate := &localGate{dir: *dir, audio: &devicevoice.AudioGate{Path: filepath.Join(*dir, "audio-state.json")}}
	if *watch != "" {
		watchdog(gate, *watch)
		return
	}
	if *restore {
		if gate.restore() != nil {
			os.Exit(1)
		}
		return
	}
	lock, err := os.OpenFile(filepath.Join(*dir, "agent.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		log.Fatal("cannot open lock")
	}
	defer lock.Close()
	if syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		log.Fatal("already running")
	}
	if gate.restore() != nil || !devicevoice.SupportedAudioService() {
		log.Fatal("audio state unavailable")
	}
	defer gate.restore()
	if *audioTest || *watchdogTest {
		start := time.Now()
		if gate.mute() != nil {
			log.Fatal("mute failed")
		}
		log.Printf("local_gate_ms=%d", time.Since(start).Milliseconds())
		if *watchdogTest {
			time.Sleep(29 * time.Second)
			if _, err := os.Stat(filepath.Join(*dir, "audio-state.json")); !os.IsNotExist(err) {
				log.Fatal("watchdog did not restore")
			}
			log.Print("independent_watchdog_passed")
		} else {
			time.Sleep(500 * time.Millisecond)
			if gate.restore() != nil {
				log.Fatal("restore failed")
			}
			log.Print("audio_test_passed")
		}
		return
	}
	if runtimeenv.ConfigureDNSFromEnv() != nil {
		log.Fatal("invalid DNS")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	model := &chatgptdirect.Client{Path: filepath.Join(*dir, "chatgpt-account.json")}
	if err = model.Available(ctx); err != nil {
		log.Fatal(err)
	}
	cfg, err := xiaobot.NewConfigFromFile(filepath.Join(*dir, "speaker-config.json"))
	if err != nil || cfg == nil || cfg.NeedSetup() || cfg.Hardware != "LX04" {
		log.Fatal("incomplete Xiaomi configuration")
	}
	cfg.TokenPath = filepath.Join(*dir, ".mi.token")
	box := xiaobot.NewXiaoMi(cfg)
	if box.InitMiBox() != nil {
		log.Fatal("Xiaomi session unavailable")
	}
	if *selfTest {
		start := time.Now()
		answer, err := model.Answer(ctx, "用一句话说：音箱已经能够独立连接 GPT，不需要 Mac 转发。")
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("selftest_model_seconds=%.2f", time.Since(start).Seconds())
		if box.MiTTS(devicevoice.SpeechText(answer)) != nil {
			log.Fatal("speech test failed")
		}
		log.Print("selftest_tts_accepted")
		return
	}
	a := &agent{gate: gate, model: model, box: box, ctx: ctx}
	for ctx.Err() == nil {
		cmd := exec.CommandContext(ctx, "/system/bin/logcat", "-v", "raw", "-T", time.Now().Format("01-02 15:04:05.000"), "MICO.cap:I", "MICO.speech:I", "*:S")
		pipe, err := cmd.StdoutPipe()
		if err != nil {
			break
		}
		cmd.Stderr = io.Discard
		if cmd.Start() != nil {
			break
		}
		log.Print("ready: standalone_gpt-6-luna native_asr local_audio_gate")
		scan := bufio.NewScanner(pipe)
		scan.Buffer(make([]byte, 65536), 1<<20)
		for scan.Scan() {
			a.event(parse(scan.Text()))
		}
		cmd.Wait()
		a.mu.Lock()
		if a.cancel != nil {
			a.cancel()
		}
		a.restore()
		a.mu.Unlock()
		select {
		case <-ctx.Done():
		case <-time.After(3 * time.Second):
		}
	}
}
