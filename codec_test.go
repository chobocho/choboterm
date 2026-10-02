package main

import (
	"bytes"
	"testing"
)

// "한글 ─│" in CP949.
var euckrSample = []byte{0xC7, 0xD1, 0xB1, 0xDB, ' ', 0xA6, 0xA1, 0xA6, 0xA2}

func TestCodecDecodeSplitChunks(t *testing.T) {
	c := newCodec(EncodingEUCKR)
	var got []byte
	// Feed one byte at a time so every multibyte character is split.
	for _, b := range euckrSample {
		got = append(got, c.Decode([]byte{b})...)
	}
	if string(got) != "한글 ─│" {
		t.Fatalf("got %q", got)
	}
}

func TestCodecDecodeCP949Extension(t *testing.T) {
	c := newCodec(EncodingEUCKR)
	// "똠" exists only in CP949 (UHC), not in pure EUC-KR.
	if got := c.Decode([]byte{0x8C, 0x63}); string(got) != "똠" {
		t.Fatalf("got %q", got)
	}
}

func TestCodecEncode(t *testing.T) {
	c := newCodec(EncodingEUCKR)
	if got := c.Encode("한글 ─│"); !bytes.Equal(got, euckrSample) {
		t.Fatalf("got % X", got)
	}
	if got := c.Encode("a😀b"); string(got) != "a?b" {
		t.Fatalf("unsupported char: got %q", got)
	}
}

func TestCodecUTF8Passthrough(t *testing.T) {
	c := newCodec("")
	if c.Name() != EncodingUTF8 {
		t.Fatalf("name %q", c.Name())
	}
	in := []byte("한글\x1b[0m")
	if got := c.Decode(in); !bytes.Equal(got, in) {
		t.Fatalf("got %q", got)
	}
}
