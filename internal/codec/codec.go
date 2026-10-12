package codec

import (
	"sync"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/transform"
)

const (
	UTF8  = "UTF-8"
	EUCKR = "EUC-KR"
)

// Codec converts between the remote character set and UTF-8 (used by xterm.js).
// EUC-KR is handled as CP949 (a superset), which is what Korean servers actually send.
type Codec struct {
	mu    sync.Mutex
	name  string
	dec   transform.Transformer
	enc   *encoding.Encoder
	carry []byte // incomplete multibyte sequence left from the previous chunk
}

func New(name string) *Codec {
	c := &Codec{}
	c.Set(name)
	return c
}

// Set switches the character set and returns the normalized name.
func (c *Codec) Set(name string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if name != EUCKR {
		name = UTF8
	}
	c.name = name
	c.carry = nil
	if name == EUCKR {
		c.dec = korean.EUCKR.NewDecoder()
		c.enc = korean.EUCKR.NewEncoder()
	} else {
		c.dec, c.enc = nil, nil
	}
	return name
}

func (c *Codec) Name() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.name
}

// Decode converts remote bytes to UTF-8. A multibyte character split across
// chunks is kept and completed with the next chunk.
func (c *Codec) Decode(b []byte) []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dec == nil {
		return b
	}
	src := append(c.carry, b...)
	dst := make([]byte, len(src)*2+16)
	nDst, nSrc, _ := c.dec.Transform(dst, src, false)
	c.carry = append([]byte(nil), src[nSrc:]...)
	if len(c.carry) > 4 {
		// Should not happen with a stateless decoder; never let it grow.
		dst = append(dst[:nDst], c.carry...)
		nDst = len(dst)
		c.carry = nil
	}
	return dst[:nDst]
}

// Encode converts UTF-8 keyboard input to the remote character set.
// Characters that don't exist in CP949 become '?'.
func (c *Codec) Encode(s string) []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.enc == nil {
		return []byte(s)
	}
	if out, err := c.enc.String(s); err == nil {
		return []byte(out)
	}
	// encoding.ReplaceUnsupported would emit 0x1A (Ctrl+Z) to the remote, so replace per rune instead.
	var out []byte
	for _, r := range s {
		if b, err := c.enc.String(string(r)); err == nil {
			out = append(out, b...)
		} else {
			out = append(out, '?')
		}
	}
	return out
}

// DecodeString converts a complete remote string (e.g. a file name) to UTF-8.
func (c *Codec) DecodeString(s string) string {
	c.mu.Lock()
	euckr := c.name == EUCKR
	c.mu.Unlock()
	if !euckr {
		return s
	}
	out, err := korean.EUCKR.NewDecoder().String(s)
	if err != nil {
		return s
	}
	return out
}

// EncodeString converts a complete UTF-8 string (e.g. a file path) to the remote set.
func (c *Codec) EncodeString(s string) string {
	return string(c.Encode(s))
}
