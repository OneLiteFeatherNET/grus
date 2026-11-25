package mcproto

import (
	"bufio"
	"bytes"
	"errors"
	"testing"
)

func TestStringRoundTripKeepsText(t *testing.T) {
	const text = "Grus stub äöü"
	d := NewDecoder(AppendString(nil, text))
	got, err := d.String(MaxStringBytes)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got != text {
		t.Errorf("string = %q, want %q", got, text)
	}
	if d.Remaining() != 0 {
		t.Errorf("remaining = %d, want 0", d.Remaining())
	}
}

func TestStringRejectsLengthAboveLimit(t *testing.T) {
	d := NewDecoder(AppendString(nil, "abcdef"))
	if _, err := d.String(3); err == nil {
		t.Fatal("string above the limit decoded without error")
	}
}

func TestLongAndUShortRoundTrip(t *testing.T) {
	var b []byte
	b = AppendUShort(b, 25565)
	b = AppendLong(b, -42)
	d := NewDecoder(b)
	port, err := d.UShort()
	if err != nil || port != 25565 {
		t.Fatalf("ushort = %d, %v", port, err)
	}
	v, err := d.Long()
	if err != nil || v != -42 {
		t.Fatalf("long = %d, %v", v, err)
	}
}

func TestPacketFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	payload := AppendString(nil, "hello")
	if err := WritePacket(&buf, 0x00, payload); err != nil {
		t.Fatalf("write: %v", err)
	}
	pkt, err := ReadPacket(bufio.NewReader(&buf), MaxPacketLength)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if pkt.ID != 0x00 {
		t.Errorf("id = %d, want 0", pkt.ID)
	}
	if !bytes.Equal(pkt.Data, payload) {
		t.Errorf("data = % x, want % x", pkt.Data, payload)
	}
}

func TestReadPacketRejectsOversizedFrame(t *testing.T) {
	var buf bytes.Buffer
	if err := WritePacket(&buf, 0x01, make([]byte, 64)); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := ReadPacket(bufio.NewReader(&buf), 16)
	if !errors.Is(err, ErrPacketTooLarge) {
		t.Fatalf("err = %v, want ErrPacketTooLarge", err)
	}
}

func TestReadPacketRejectsZeroLength(t *testing.T) {
	_, err := ReadPacket(bufio.NewReader(bytes.NewReader([]byte{0x00})), MaxPacketLength)
	if !errors.Is(err, ErrPacketTooLarge) {
		t.Fatalf("err = %v, want ErrPacketTooLarge", err)
	}
}
