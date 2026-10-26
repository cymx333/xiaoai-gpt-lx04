// SPDX-License-Identifier: Apache-2.0
// Minimal outbound ADB transport for the user's existing, root-enabled LX04.
package hostlink

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

type Device struct{ Address, Serial string }
type packet struct {
	kind string
	a, b uint32
	data []byte
}

func send(c net.Conn, kind string, a, b uint32, data []byte) error {
	h := make([]byte, 24)
	n := binary.LittleEndian.Uint32([]byte(kind))
	sum := uint32(0)
	for _, v := range data {
		sum += uint32(v)
	}
	for i, v := range []uint32{n, a, b, uint32(len(data)), sum, n ^ 0xffffffff} {
		binary.LittleEndian.PutUint32(h[i*4:], v)
	}
	_, err := io.Copy(c, strings.NewReader(string(append(h, data...))))
	return err
}
func receive(c net.Conn) (packet, error) {
	var p packet
	h := make([]byte, 24)
	if _, err := io.ReadFull(c, h); err != nil {
		return p, err
	}
	n := binary.LittleEndian.Uint32(h)
	size := binary.LittleEndian.Uint32(h[12:])
	if size > 1024*1024 || binary.LittleEndian.Uint32(h[20:]) != n^0xffffffff {
		return p, errors.New("invalid ADB packet")
	}
	p.kind = string(h[:4])
	p.a = binary.LittleEndian.Uint32(h[4:])
	p.b = binary.LittleEndian.Uint32(h[8:])
	p.data = make([]byte, size)
	_, err := io.ReadFull(c, p.data)
	return p, err
}
func stream(c net.Conn, local uint32, command string, consume func([]byte) error) error {
	if err := send(c, "OPEN", local, 0, append([]byte("shell:"+command), 0)); err != nil {
		return err
	}
	for {
		p, err := receive(c)
		if err != nil {
			return err
		}
		if p.b != local {
			continue
		}
		switch p.kind {
		case "OKAY":
		case "WRTE":
			if err := send(c, "OKAY", local, p.a, nil); err != nil {
				return err
			}
			if err := consume(p.data); err != nil {
				return err
			}
		case "CLSE":
			send(c, "CLSE", local, p.a, nil)
			return nil
		default:
			return errors.New("unexpected ADB response")
		}
	}
}
func collect(c net.Conn, command string) (string, error) {
	var b strings.Builder
	err := stream(c, 1, command, func(p []byte) error {
		if b.Len()+len(p) > 65536 {
			return errors.New("ADB output limit")
		}
		b.Write(p)
		return nil
	})
	return b.String(), err
}
func (d Device) Connect(ctx context.Context) (net.Conn, error) {
	if d.Serial == "" {
		return nil, errors.New("exact device serial is required")
	}
	c, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "tcp", d.Address)
	if err != nil {
		return nil, err
	}
	c.SetDeadline(time.Now().Add(5 * time.Second))
	fail := func(e error) (net.Conn, error) { c.Close(); return nil, e }
	if err = send(c, "CNXN", 0x01000000, 4096, []byte("host::\x00")); err != nil {
		return fail(err)
	}
	p, err := receive(c)
	if err != nil {
		return fail(err)
	}
	if p.kind != "CNXN" || !strings.Contains(string(p.data), "ro.product.model=LX04;") {
		return fail(errors.New("target is not the expected root-enabled LX04"))
	}
	serial, err := collect(c, "getprop ro.serialno")
	if err != nil {
		return fail(err)
	}
	if strings.TrimSpace(serial) != d.Serial {
		return fail(errors.New("speaker serial mismatch; refusing device operation"))
	}
	c.SetDeadline(time.Time{})
	return c, nil
}
func (d Device) Stream(ctx context.Context, command string, consume func([]byte) error) error {
	c, err := d.Connect(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			c.Close()
		case <-done:
		}
	}()
	return stream(c, 2, command, consume)
}
func (d Device) Shell(ctx context.Context, command string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	var b strings.Builder
	err := d.Stream(ctx, command, func(p []byte) error {
		if b.Len()+len(p) > 65536 {
			return fmt.Errorf("ADB output limit")
		}
		b.Write(p)
		return nil
	})
	return b.String(), err
}
