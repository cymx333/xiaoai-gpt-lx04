// SPDX-License-Identifier: Apache-2.0
package hostlink

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
)

const watchFile = "/data/local/tmp/xiaoai-gpt-watchdog"

var parcel = regexp.MustCompile(`Parcel\(\s*00000000\s+(0000000[01])`)

type audioState struct {
	Serial string `json:"serial"`
	Muted  bool   `json:"muted"`
}
type Audio struct {
	Device Device
	Path   string
	mu     sync.Mutex
}

func (a *Audio) Current(ctx context.Context) (bool, error) {
	s, err := a.Device.Shell(ctx, "service call audio 6")
	if err != nil {
		return false, err
	}
	m := parcel.FindStringSubmatch(s)
	if len(m) != 2 {
		return false, fmt.Errorf("unsupported audio state reply: %q", s)
	}
	return m[1] == "00000001", nil
}
func muteCommand(muted bool) string {
	v := 0
	if muted {
		v = 1
	}
	return fmt.Sprintf("service call audio 7 i32 %d i32 0 s16 com.android.shell i32 0", v)
}
func (a *Audio) Mute(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	state := audioState{Serial: a.Device.Serial}
	b, err := os.ReadFile(a.Path)
	if err == nil {
		if json.Unmarshal(b, &state) != nil || state.Serial != a.Device.Serial {
			return errors.New("invalid audio recovery state")
		}
	} else if os.IsNotExist(err) {
		state.Muted, err = a.Current(ctx)
		if err != nil {
			return err
		}
		b, _ = json.Marshal(state)
		if err = os.WriteFile(a.Path, b, 0600); err != nil {
			return err
		}
	} else {
		return err
	}
	nonce := make([]byte, 16)
	if _, err = rand.Read(nonce); err != nil {
		return err
	}
	token := hex.EncodeToString(nonce)
	// The watchdog stays on the speaker and restores its prior state after a Mac crash.
	// This firmware can kill a just-forked child when the ADB stream closes.
	// Wait for a nonce written *after* setsid, rather than assuming '&' is enough.
	armedFile := watchFile + ".armed"
	watcher := fmt.Sprintf("printf %%s %s > %s || exit 1; sleep 25; if [ \"$(cat %s 2>/dev/null)\" = %s ]; then %s; rm -f %s %s; fi", token, armedFile, watchFile, token, muteCommand(state.Muted), watchFile, armedFile)
	cmd := fmt.Sprintf("umask 077; printf %%s %s > %s || exit 1; setsid /system/bin/sh -c '%s' </dev/null >/dev/null 2>&1 & i=0; while [ $i -lt 20 ]; do if [ \"$(cat %s 2>/dev/null)\" = %s ]; then echo WATCHDOG_ARMED; break; fi; sleep 0.1; i=$((i+1)); done", token, watchFile, watcher, armedFile, token)
	armed, err := a.Device.Shell(ctx, cmd)
	if err != nil {
		return err
	}
	if !strings.Contains(armed, "WATCHDOG_ARMED") {
		return errors.New("watchdog did not arm")
	}
	s, err := a.Device.Shell(ctx, muteCommand(true))
	if err != nil {
		return err
	}
	if !strings.Contains(s, "00000000") {
		return errors.New("mute rejected")
	}
	return nil
}
func (a *Audio) Restore(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	b, err := os.ReadFile(a.Path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var state audioState
	if json.Unmarshal(b, &state) != nil || state.Serial != a.Device.Serial {
		return errors.New("invalid audio recovery state")
	}
	s, err := a.Device.Shell(ctx, muteCommand(state.Muted))
	if err != nil {
		return err
	}
	if !strings.Contains(s, "00000000") {
		return errors.New("restore rejected")
	}
	if _, err = a.Device.Shell(ctx, "rm -f "+watchFile+" "+watchFile+".armed"); err != nil {
		return err
	}
	return os.Remove(a.Path)
}
