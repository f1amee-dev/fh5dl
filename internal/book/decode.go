package book

import (
	"crypto/rc4" // Viewer obfuscation compatibility, not a security primitive.
	"fmt"
	"strconv"
	"strings"
)

// DecodeString ports the v01 DeString wire format used by the public viewer.
// The header contains a block count followed by six-hex-digit descriptors.
// Bit 20 marks encoded blocks; low 20 bits give their byte length. Encoded
// blocks end with a nibble-pair key and one byte giving that key's length.
func DecodeString(s string) ([]byte, error) {
	if len(s) < 7 || !strings.HasPrefix(s, "v01") {
		return nil, fmt.Errorf("unsupported encoded config version")
	}
	count, err := strconv.ParseUint(s[3:7], 16, 16)
	if err != nil {
		return nil, fmt.Errorf("invalid block count: %w", err)
	}
	pos := 7 + int(count)*6
	if pos > len(s) {
		return nil, fmt.Errorf("truncated block header")
	}
	out := make([]byte, 0, len(s))
	for i := 0; i < int(count); i++ {
		d, err := strconv.ParseUint(s[7+i*6:13+i*6], 16, 24)
		if err != nil || d & ^uint64(0x1fffff) != 0 {
			return nil, fmt.Errorf("invalid block descriptor %d", i)
		}
		n := int(d & 0xfffff)
		if n > len(s)-pos {
			return nil, fmt.Errorf("truncated block %d", i)
		}
		b := []byte(s[pos : pos+n])
		pos += n
		if d&0x100000 != 0 {
			if len(b) < 2 {
				return nil, fmt.Errorf("short encoded block")
			}
			tail, err := decodePairs(b[len(b)-2:])
			if err != nil {
				return nil, err
			}
			split := len(b) - 2 - 2*int(tail[0])
			if split < 0 || tail[0] == 0 {
				return nil, fmt.Errorf("invalid encoded key length")
			}
			key, err := decodePairs(b[split : len(b)-2])
			if err != nil {
				return nil, err
			}
			b, err = decodePairs(b[:split])
			if err != nil {
				return nil, err
			}
			c, err := rc4.NewCipher(key)
			if err != nil {
				return nil, err
			}
			c.XORKeyStream(b, b)
		}
		out = append(out, b...)
	}
	// The viewer's output can include a trailing marker after the JSON value.
	return append(out, s[pos:]...), nil
}

func decodePairs(s []byte) ([]byte, error) {
	if len(s)%2 != 0 {
		return nil, fmt.Errorf("odd nibble-pair length")
	}
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_#"
	b := make([]byte, len(s)/2)
	for i := range b {
		a, c := strings.IndexByte(alphabet, s[2*i]), strings.IndexByte(alphabet, s[2*i+1])
		if a < 0 || c < 0 {
			return nil, fmt.Errorf("invalid nibble-pair character")
		}
		b[i] = byte((a&15)<<4 | c&15)
	}
	return b, nil
}
