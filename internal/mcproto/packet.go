// Package mcproto implements the parts of the Minecraft Java edition protocol a server needs to answer
// the server list (status, ping) and to refuse logins politely: VarInt and string framing, packets, and
// the handshake state machine.
package mcproto

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"
)

// MaxPacketLength is the largest packet body the protocol allows (2^21 - 1 bytes).
const MaxPacketLength = 2097151

// MaxStringBytes bounds a protocol string: 32767 characters at up to three bytes each.
const MaxStringBytes = 32767 * 3

// ErrPacketTooLarge reports a frame whose declared length is zero, negative or above the limit.
var ErrPacketTooLarge = errors.New("mcproto: packet length out of range")

// AppendVarInt appends v as a protocol VarInt (7 bits per byte, least significant group first).
func AppendVarInt(b []byte, v int32) []byte {
	u := uint32(v)
	for u&^0x7f != 0 {
		b = append(b, byte(u&0x7f|0x80))
		u >>= 7
	}
	return append(b, byte(u))
}

// AppendString appends s as a VarInt length followed by its UTF-8 bytes.
func AppendString(b []byte, s string) []byte {
	b = AppendVarInt(b, int32(len(s)))
	return append(b, s...)
}

// AppendUShort appends v as a big-endian unsigned 16-bit integer.
func AppendUShort(b []byte, v uint16) []byte {
	return binary.BigEndian.AppendUint16(b, v)
}

// AppendLong appends v as a big-endian signed 64-bit integer.
func AppendLong(b []byte, v int64) []byte {
	return binary.BigEndian.AppendUint64(b, uint64(v))
}

// Decoder reads protocol values from a byte slice that holds one packet body.
type Decoder struct {
	r *bytes.Reader
}

// NewDecoder returns a Decoder over p.
func NewDecoder(p []byte) *Decoder {
	return &Decoder{r: bytes.NewReader(p)}
}

// Remaining returns the number of unread bytes.
func (d *Decoder) Remaining() int {
	return d.r.Len()
}

// VarInt reads a VarInt.
func (d *Decoder) VarInt() (int32, error) {
	return ReadVarInt(d.r)
}

// UShort reads a big-endian unsigned 16-bit integer.
func (d *Decoder) UShort() (uint16, error) {
	var b [2]byte
	if _, err := io.ReadFull(d.r, b[:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(b[:]), nil
}

// Long reads a big-endian signed 64-bit integer.
func (d *Decoder) Long() (int64, error) {
	var b [8]byte
	if _, err := io.ReadFull(d.r, b[:]); err != nil {
		return 0, err
	}
	return int64(binary.BigEndian.Uint64(b[:])), nil
}

// String reads a length-prefixed UTF-8 string whose byte length may not exceed maxBytes.
func (d *Decoder) String(maxBytes int) (string, error) {
	n, err := d.VarInt()
	if err != nil {
		return "", err
	}
	if n < 0 || int(n) > maxBytes || int(n) > d.r.Len() {
		return "", fmt.Errorf("mcproto: string length %d out of range", n)
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(d.r, b); err != nil {
		return "", err
	}
	if !utf8.Valid(b) {
		return "", errors.New("mcproto: string is not valid UTF-8")
	}
	return string(b), nil
}

// Packet is one decoded frame: the packet id and the body that follows it.
type Packet struct {
	ID   int32
	Data []byte
}

// WritePacket frames a packet as VarInt length, VarInt id and payload, and writes it in one call.
func WritePacket(w io.Writer, id int32, payload []byte) error {
	body := AppendVarInt(nil, id)
	body = append(body, payload...)
	frame := AppendVarInt(nil, int32(len(body)))
	frame = append(frame, body...)
	_, err := w.Write(frame)
	return err
}

// ReadPacket reads one frame. A declared length of zero or above maxLen is rejected before any body
// bytes are read.
func ReadPacket(r *bufio.Reader, maxLen int32) (Packet, error) {
	n, err := ReadVarInt(r)
	if err != nil {
		return Packet{}, err
	}
	if n <= 0 || n > maxLen {
		return Packet{}, ErrPacketTooLarge
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return Packet{}, err
	}
	d := NewDecoder(body)
	id, err := d.VarInt()
	if err != nil {
		return Packet{}, err
	}
	return Packet{ID: id, Data: body[len(body)-d.Remaining():]}, nil
}
