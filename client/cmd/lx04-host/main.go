// SPDX-License-Identifier: Apache-2.0
// Mac companion: upstream LX04 event routing and Xiaomi TTS, managed ChatGPT login.
package main

import (
	"bufio"
	"context"
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
	"xiaobot/devicevoice"
	"xiaobot/hostlink"
)

type modelReply struct {
	Ready   bool    `json:"ready"`
	Text    string  `json:"text"`
	Error   string  `json:"error"`
	Elapsed float64 `json:"elapsed_seconds"`
}
type modelProcess struct {
	mu             sync.Mutex
	python, script string
	cmd            *exec.Cmd
	in             io.WriteCloser
	replies        chan modelReply
	exit           chan struct{}
}

func (m *modelProcess) stop() {
	if m.cmd != nil {
		close(m.exit)
		syscall.Kill(-m.cmd.Process.Pid, syscall.SIGTERM)
		m.cmd.Wait()
		m.cmd = nil
	}
}
func (m *modelProcess) start(ctx context.Context) error {
	if m.cmd != nil {
		return nil
	}
	cmd := exec.Command(m.python, m.script)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stderr = os.Stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err = cmd.Start(); err != nil {
		return err
	}
	m.cmd = cmd
	m.in = in
	ch := make(chan modelReply, 2)
	m.replies = ch
	done := make(chan struct{})
	m.exit = done
	go func() {
		defer close(ch)
		s := bufio.NewScanner(out)
		s.Buffer(make([]byte, 4096), 65536)
		for s.Scan() {
			var r modelReply
			if json.Unmarshal(s.Bytes(), &r) == nil {
				select {
				case ch <- r:
				case <-done:
					return
				}
			}
		}
	}()
	select {
	case r, ok := <-ch:
		if !ok || !r.Ready {
			m.stop()
			return errors.New("GPT worker unavailable")
		}
		return nil
	case <-ctx.Done():
		m.stop()
		return ctx.Err()
	}
}
func (m *modelProcess) warm(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.start(ctx)
}
func (m *modelProcess) answer(ctx context.Context, q string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.start(ctx); err != nil {
		return "", err
	}
	b, _ := json.Marshal(map[string]string{"text": q})
	if _, err := m.in.Write(append(b, '\n')); err != nil {
		m.stop()
		return "", err
	}
	select {
	case r, ok := <-m.replies:
		if !ok || r.Error != "" || r.Text == "" {
			m.stop()
			return "", errors.New("GPT answer failed")
		}
		log.Printf("model=gpt-6-luna answer_seconds=%.2f", r.Elapsed)
		return r.Text, nil
	case <-ctx.Done():
		m.stop()
		return "", ctx.Err()
	}
}

type speaker interface{ MiTTS(string) error }
type host struct {
	mu            sync.Mutex
	ttsMu         sync.Mutex
	gate          *hostlink.Audio
	model         *modelProcess
	box           speaker
	generation    int
	cancel        context.CancelFunc
	pending, last string
	lastAt        time.Time
	ctx           context.Context
}

func (h *host) restore() {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	if h.gate.Restore(ctx) != nil {
		log.Print("audio_restore_failed; speaker watchdog remains armed")
	}
}
func (h *host) event(e devicevoice.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if e.Kind == "speak" {
		if h.pending != "" && strings.TrimSpace(e.Text) == h.pending {
			h.pending = ""
			h.restore()
			log.Print("GPT speech observed on speaker")
		}
		return
	}
	if e.Kind != "query" {
		return
	}
	q := strings.TrimSpace(e.Text)
	if q == "" || len([]rune(q)) > 2000 {
		return
	}
	if q == h.last && time.Since(h.lastAt) < 2*time.Second {
		return
	}
	h.last = q
	h.lastAt = time.Now()
	h.generation++
	generation := h.generation
	if h.cancel != nil {
		h.cancel()
	}
	h.pending = ""
	if devicevoice.NativeRequest(q) {
		h.restore()
		log.Print("route=native")
		return
	}
	ctx, cancel := context.WithTimeout(h.ctx, 23*time.Second)
	h.cancel = cancel
	if err := h.gate.Mute(ctx); err != nil {
		cancel()
		h.restore()
		log.Print("audio_gate_unavailable; native answer retained")
		return
	}
	log.Print("route=gpt-6-luna")
	go h.answer(ctx, generation, q)
	go func() {
		<-ctx.Done()
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.generation == generation {
			h.pending = ""
			h.restore()
		}
	}()
}
func (h *host) answer(ctx context.Context, generation int, q string) {
	answer, err := h.model.answer(ctx, q)
	if ctx.Err() != nil {
		return
	}
	if err != nil {
		answer = "这次 GPT 连接没有成功，请稍后再试。"
		log.Print("GPT request failed")
	}
	answer = devicevoice.SpeechText(answer)
	h.ttsMu.Lock()
	defer h.ttsMu.Unlock()
	h.mu.Lock()
	if h.generation != generation || ctx.Err() != nil {
		h.mu.Unlock()
		return
	}
	h.pending = answer
	h.mu.Unlock()
	// Never hold the event lock while making a cloud request. Already-dispatched
	// Xiaomi speech cannot be recalled, but stale queued requests are discarded.
	err = h.box.MiTTS(answer)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.generation != generation {
		return
	}
	if err != nil {
		h.pending = ""
		h.restore()
		log.Print("xiaomi_tts_failed")
		return
	}
	log.Print("xiaomi_tts_accepted")
	go func() {
		timer := time.NewTimer(time.Second)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return
		}
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.generation == generation && h.pending != "" {
			h.pending = ""
			h.restore()
		}
	}()
}

func main() {
	configPath := flag.String("config", "", "private Xiaomi config")
	serial := flag.String("serial", "", "exact speaker serial")
	address := flag.String("address", "", "required speaker ADB address (IP:port)")
	script := flag.String("worker", "", "Codex voice worker path")
	python := flag.String("python", "python3", "Python 3.11+ path")
	probe := flag.Bool("probe", false, "read-only device compatibility check")
	audioTest := flag.Bool("audio-test", false, "test mute and restore, with on-speaker watchdog")
	watchdogTest := flag.Bool("watchdog-test", false, "verify the speaker restores audio after 25 seconds without Mac intervention")
	speakTest := flag.Bool("speak-test", false, "test Xiaomi native speech")
	restoreOnly := flag.Bool("restore-audio", false, "restore a saved audio state")
	flag.Parse()
	log.SetFlags(log.Ldate | log.Ltime)
	if *serial == "" || *configPath == "" {
		log.Fatal("-serial and -config are required")
	}
	device := hostlink.Device{Address: *address, Serial: *serial}
	gate := &hostlink.Audio{Device: device, Path: filepath.Join(filepath.Dir(*configPath), ".host-audio-restore.json")}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	if *probe {
		check, c := context.WithTimeout(ctx, 8*time.Second)
		defer c()
		out, err := device.Shell(check, "id -u; getprop ro.build.version.sdk; getprop ro.product.model")
		if err != nil {
			log.Fatal("device preflight failed")
		}
		muted, err := gate.Current(check)
		if err != nil {
			log.Fatalf("audio preflight failed: %v", err)
		}
		fmt.Printf("device=%q master_muted=%v\n", strings.TrimSpace(out), muted)
		return
	}
	lock, err := os.OpenFile(filepath.Join(filepath.Dir(*configPath), "host.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		log.Fatal("cannot open runtime lock")
	}
	defer lock.Close()
	if syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		log.Fatal("another host instance is active")
	}
	if gate.Restore(ctx) != nil {
		log.Fatal("saved audio recovery failed")
	}
	defer func() {
		c, stop := context.WithTimeout(context.Background(), 6*time.Second)
		defer stop()
		gate.Restore(c)
	}()
	if *restoreOnly {
		return
	}
	if *audioTest || *watchdogTest {
		before, err := gate.Current(ctx)
		if err != nil {
			log.Fatal("audio read failed")
		}
		if gate.Mute(ctx) != nil {
			log.Fatal("mute test failed")
		}
		during, err := gate.Current(ctx)
		if err != nil {
			log.Fatal("audio verification failed")
		}
		if *watchdogTest {
			timer := time.NewTimer(28 * time.Second)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				gate.Restore(context.Background())
				return
			}
			recovered, recoveryErr := gate.Current(ctx)
			if recoveryErr != nil || recovered != before {
				log.Fatal("speaker watchdog recovery failed")
			}
			fmt.Println("speaker watchdog independently restored the original audio state")
		}
		if gate.Restore(ctx) != nil {
			log.Fatal("restore failed; watchdog will restore")
		}
		after, err := gate.Current(ctx)
		if err != nil || !during || before != after {
			log.Fatal("audio test did not preserve state")
		}
		fmt.Println("audio mute/restore passed; original state preserved")
		return
	}
	cfg, err := xiaobot.NewConfigFromFile(*configPath)
	if err != nil || cfg.NeedSetup() || cfg.Hardware != "LX04" {
		log.Fatal("Xiaomi login setup required")
	}
	cfg.TokenPath = filepath.Join(filepath.Dir(*configPath), ".mi.token")
	box := xiaobot.NewXiaoMi(cfg)
	if box.InitMiBox() != nil {
		log.Fatal("Xiaomi session unavailable; run bootstrap")
	}
	if *speakTest {
		if box.MiTTS("GPT 音箱接入测试，语音播报连接成功。") != nil {
			log.Fatal("speech test failed")
		}
		fmt.Println("Xiaomi speech test accepted")
		return
	}
	if *script == "" {
		log.Fatal("-worker is required")
	}
	model := &modelProcess{python: *python, script: *script}
	defer func() { model.mu.Lock(); defer model.mu.Unlock(); model.stop() }()
	warm, warmCancel := context.WithTimeout(ctx, 35*time.Second)
	err = model.warm(warm)
	warmCancel()
	if err != nil {
		log.Fatal("ChatGPT worker unavailable")
	}
	h := &host{gate: gate, model: model, box: box, ctx: ctx}
	log.Print("ready: LX04 native ASR / GPT-6 Luna / Xiaomi native speech")
	for ctx.Err() == nil {
		var line strings.Builder
		err = device.Stream(ctx, `logcat -v raw -T "$(date '+%m-%d %H:%M:%S.000')" MICO.cap:I MICO.speech:I '*:S'`, func(chunk []byte) error {
			line.Write(chunk)
			if line.Len() > 1024*1024 {
				return errors.New("speech event too large")
			}
			data := line.String()
			parts := strings.Split(data, "\n")
			line.Reset()
			line.WriteString(parts[len(parts)-1])
			for _, s := range parts[:len(parts)-1] {
				h.event(devicevoice.ParseEvent(s))
			}
			return nil
		})
		h.mu.Lock()
		h.generation++
		if h.cancel != nil {
			h.cancel()
		}
		h.pending = ""
		h.restore()
		h.mu.Unlock()
		if ctx.Err() != nil {
			break
		}
		if err != nil {
			log.Printf("speaker listener ended: %v; retrying in 3 seconds", err)
		}
		select {
		case <-ctx.Done():
		case <-time.After(3 * time.Second):
		}
	}
}
