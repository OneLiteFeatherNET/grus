package mcproto

import (
	"bufio"
	"bytes"
	"errors"
	"testing"
)

func TestVarIntRoundTripKeepsValue(t *testing.T) {
	values := []int32{0, 1, 127, 128, 255, 25565, 2097151, 1 << 30, -1, -2147483648, 2147483647}
	for _, v := range values {
		enc := AppendVarInt(nil, v)
		got, err := ReadVarInt(bufio.NewReader(bytes.NewReader(enc)))
		if err != nil {
			t.Fatalf("decode %d: %v", v, err)
		}
		if got != v {
			t.Errorf("round trip %d = %d", v, got)
		}
	}
}

func TestVarIntEncodingMatchesProtocolExamples(t *testing.T) {
	cases := map[int32][]byte{
		0:          {0x00},
		127:        {0x7f},
		128:        {0x80, 0x01},
		25565:      {0xdd, 0xc7, 0x01},
		-1:         {0xff, 0xff, 0xff, 0xff, 0x0f},
		2147483647: {0xff, 0xff, 0xff, 0xff, 0x07},
	}
	for v, want := range cases {
		if got := AppendVarInt(nil, v); !bytes.Equal(got, want) {
			t.Errorf("encode %d = % x, want % x", v, got, want)
		}
	}
}

func TestVarIntRejectsMoreThanFiveBytes(t *testing.T) {
	in := []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x01}
	_, err := ReadVarInt(bufio.NewReader(bytes.NewReader(in)))
	if !errors.Is(err, ErrVarIntTooLong) {
		t.Fatalf("err = %v, want ErrVarIntTooLong", err)
	}
}

func TestVarIntReportsEOFOnTruncatedInput(t *testing.T) {
	_, err := ReadVarInt(bufio.NewReader(bytes.NewReader([]byte{0x80})))
	if err == nil {
		t.Fatal("truncated varint decoded without error")
	}
}
