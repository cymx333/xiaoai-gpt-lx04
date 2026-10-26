package hostlink

import (
	"context"
	"net"
	"strings"
	"testing"
)

func fakeDevice(t *testing.T, serial string) (Device, <-chan string) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	commands := make(chan string, 8)
	go func() {
		defer close(commands)
		c, err := l.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		p, err := receive(c)
		if err != nil || p.kind != "CNXN" {
			return
		}
		send(c, "CNXN", 0x01000000, 4096, []byte("device::ro.product.model=LX04;"))
		for {
			p, err = receive(c)
			if err != nil {
				return
			}
			if p.kind == "CLSE" {
				continue
			}
			if p.kind != "OPEN" {
				return
			}
			cmd := strings.TrimSuffix(strings.TrimPrefix(string(p.data), "shell:"), "\x00")
			commands <- cmd
			send(c, "OKAY", 99, p.a, nil)
			out := "command-result\n"
			if cmd == "getprop ro.serialno" {
				out = serial + "\n"
			}
			send(c, "WRTE", 99, p.a, []byte(out))
			if _, err = receive(c); err != nil {
				return
			}
			send(c, "CLSE", 99, p.a, nil)
		}
	}()
	return Device{Address: l.Addr().String(), Serial: "expected-serial"}, commands
}
func TestShellVerifiesSerialBeforeCommand(t *testing.T) {
	d, commands := fakeDevice(t, "expected-serial")
	out, err := d.Shell(context.Background(), "safe-command")
	if err != nil || out != "command-result\n" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if got := <-commands; got != "getprop ro.serialno" {
		t.Fatal(got)
	}
	if got := <-commands; got != "safe-command" {
		t.Fatal(got)
	}
}
func TestSerialMismatchDoesNotExecuteCommand(t *testing.T) {
	d, commands := fakeDevice(t, "another-speaker")
	_, err := d.Shell(context.Background(), "must-not-run")
	if err == nil {
		t.Fatal("expected serial mismatch")
	}
	for command := range commands {
		if command != "getprop ro.serialno" {
			t.Fatalf("executed %q", command)
		}
	}
}
func TestCancelledConnectionStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d := Device{Address: "127.0.0.1:1", Serial: "expected"}
	if _, err := d.Shell(ctx, "must-not-run"); err == nil {
		t.Fatal("expected cancelled connection")
	}
}
