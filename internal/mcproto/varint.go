package mcproto

import (
	"errors"
	"io"
)

// MaxVarIntBytes is the longest encoding of a 32-bit VarInt.
const MaxVarIntBytes = 5

// ErrVarIntTooLong reports a VarInt that does not terminate within MaxVarIntBytes bytes.
var ErrVarIntTooLong = errors.New("mcproto: varint longer than 5 bytes")

// ReadVarInt reads a protocol VarInt. It is the inverse of AppendVarInt.
func ReadVarInt(r io.ByteReader) (int32, error) {
	var v uint32
	for i := 0; i < MaxVarIntBytes; i++ {
		c, err := r.ReadByte()
		if err != nil {
			if i > 0 && err == io.EOF {
				return 0, io.ErrUnexpectedEOF
			}
			return 0, err
		}
		v |= uint32(c&0x7f) << (7 * i)
		if c&0x80 == 0 {
			return int32(v), nil
		}
	}
	return 0, ErrVarIntTooLong
}
