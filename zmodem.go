package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ZMODEM file transfer (sz / rz on the remote side), as used by ZTerm and lrzsz.

const (
	zPAD   = '*'
	zDLE   = 0x18 // also CAN
	zBIN   = 'A'
	zHEX   = 'B'
	zBIN32 = 'C'

	zRQINIT  = 0
	zRINIT   = 1
	zSINIT   = 2
	zACK     = 3
	zFILE    = 4
	zSKIP    = 5
	zNAK     = 6
	zABORT   = 7
	zFIN     = 8
	zRPOS    = 9
	zDATA    = 10
	zEOF     = 11
	zFERR    = 12
	zCRC     = 13
	zCOMMAND = 18

	// subpacket terminators (after ZDLE)
	zCRCE = 'h' // end of frame, header follows
	zCRCG = 'i' // frame continues, no ACK
	zCRCQ = 'j' // frame continues, ZACK expected
	zCRCW = 'k' // end of frame, ZACK expected
	zRUB0 = 'l' // escaped 0x7f
	zRUB1 = 'm' // escaped 0xff

	// ZRINIT capability flags (ZF0)
	zfCANFDX  = 0x01
	zfCANOVIO = 0x02
	zfCANFC32 = 0x20
	zfESCCTL  = 0x40

	zcBIN = 1 // ZFILE ZF0: binary transfer

	zmBlock     = 1024
	zmWindow    = 64 * 1024
	zmMaxPacket = 16 * 1024
	zmRetries   = 10
)

var (
	zmSigReceive = []byte("**\x18B00") // ZRQINIT: remote sz wants to send
	zmSigSend    = []byte("**\x18B01") // ZRINIT: remote rz waits for files

	errZmTimeout      = errors.New("Zmodem 응답 시간이 초과되었습니다")
	errZmRemoteCancel = errors.New("상대편이 Zmodem 전송을 취소했습니다")
	errZmBadPacket    = errors.New("Zmodem 패킷 오류")
)

// findZmodemStart returns the index where a ZMODEM session starts in data
// (-1 if none) and whether the remote is sending (sz) rather than receiving (rz).
func findZmodemStart(data []byte) (int, bool) {
	i := bytes.Index(data, zmSigReceive)
	j := bytes.Index(data, zmSigSend)
	switch {
	case i >= 0 && (j < 0 || i < j):
		return i, true
	case j >= 0:
		return j, false
	}
	return -1, false
}

// partialSigSuffix returns how many trailing bytes of data could be the
// beginning of a ZMODEM start sequence split across reads.
func partialSigSuffix(data []byte) int {
	sig := zmSigReceive[:len(zmSigReceive)-1] // "**\x18B0" is shared by both
	for n := min(len(sig), len(data)); n > 0; n-- {
		if bytes.Equal(data[len(data)-n:], sig[:n]) {
			return n
		}
	}
	return 0
}

func crc16(crc uint16, b []byte) uint16 {
	for _, c := range b {
		crc ^= uint16(c) << 8
		for i := 0; i < 8; i++ {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

type zmHeader struct {
	typ  byte
	data [4]byte // ZP0..ZP3 (position, little endian) == ZF3..ZF0 (flags)
}

func (h zmHeader) pos() int64 { return int64(binary.LittleEndian.Uint32(h.data[:])) }
func (h zmHeader) f0() byte   { return h.data[3] }

func posHeader(typ byte, pos int64) zmHeader {
	h := zmHeader{typ: typ}
	binary.LittleEndian.PutUint32(h.data[:], uint32(pos))
	return h
}

func flagHeader(typ, f0 byte) zmHeader {
	h := zmHeader{typ: typ}
	h.data[3] = f0
	return h
}

// zmPeer is the byte transport to the remote sz/rz.
type zmPeer struct {
	ctx     context.Context
	in      <-chan []byte
	buf     []byte
	write   func([]byte) error
	escAll  bool // escape every control character (needed over Telnet)
	timeout time.Duration
	last32  bool // last header was CRC-32, so its data subpackets are too
}

// zmOptions connects the protocol to the application.
type zmOptions struct {
	Dir        string // download folder
	DecodeName func(string) string
	EncodeName func(string) string
	Progress   func(XferProgress)
}

func (p *zmPeer) readByte() (byte, error) {
	for len(p.buf) == 0 {
		t := time.NewTimer(p.timeout)
		select {
		case c, ok := <-p.in:
			t.Stop()
			if !ok {
				return 0, io.EOF
			}
			p.buf = c
		case <-t.C:
			return 0, errZmTimeout
		case <-p.ctx.Done():
			t.Stop()
			return 0, errCancelled
		}
	}
	b := p.buf[0]
	p.buf = p.buf[1:]
	return b, nil
}

// tryReadByte reads a byte only if one is already available.
func (p *zmPeer) tryReadByte() (byte, bool) {
	if len(p.buf) == 0 {
		select {
		case c, ok := <-p.in:
			if !ok || len(c) == 0 {
				return 0, false
			}
			p.buf = c
		default:
			return 0, false
		}
	}
	b := p.buf[0]
	p.buf = p.buf[1:]
	return b, true
}

func (p *zmPeer) unread(b byte) {
	p.buf = append([]byte{b}, p.buf...)
}

func isFlowCtl(c byte) bool { return c == 0x11 || c == 0x13 || c == 0x91 || c == 0x93 }

// readEscaped decodes one ZDLE-escaped byte. At a subpacket terminator it
// returns the terminator and end=true.
func (p *zmPeer) readEscaped() (c byte, end bool, err error) {
	for {
		if c, err = p.readByte(); err != nil {
			return 0, false, err
		}
		if isFlowCtl(c) {
			continue
		}
		if c != zDLE {
			return c, false, nil
		}
		cans := 1
		for {
			if c, err = p.readByte(); err != nil {
				return 0, false, err
			}
			if isFlowCtl(c) {
				continue
			}
			if c != zDLE {
				break
			}
			if cans++; cans >= 5 {
				return 0, false, errZmRemoteCancel
			}
		}
		switch {
		case c >= zCRCE && c <= zCRCW:
			return c, true, nil
		case c == zRUB0:
			return 0x7f, false, nil
		case c == zRUB1:
			return 0xff, false, nil
		case c&0x60 == 0x40:
			return c ^ 0x40, false, nil
		}
		return 0, false, errZmBadPacket
	}
}

// readHeader skips noise until a valid header arrives.
func (p *zmPeer) readHeader() (zmHeader, error) {
	cans := 0
	for {
		c, err := p.readByte()
		if err != nil {
			return zmHeader{}, err
		}
		if c == zDLE {
			if cans++; cans >= 5 {
				return zmHeader{}, errZmRemoteCancel
			}
			continue
		}
		cans = 0
		if c != zPAD {
			continue
		}
		for c == zPAD {
			if c, err = p.readByte(); err != nil {
				return zmHeader{}, err
			}
		}
		if c != zDLE {
			p.unread(c)
			continue
		}
		f, err := p.readByte()
		if err != nil {
			return zmHeader{}, err
		}
		var h zmHeader
		switch f {
		case zHEX:
			h, err = p.readHexHeader()
		case zBIN:
			h, err = p.readBinHeader(false)
		case zBIN32:
			h, err = p.readBinHeader(true)
		default:
			continue
		}
		if errors.Is(err, errZmBadPacket) {
			continue // corrupted header: keep looking, the peer will repeat it
		}
		return h, err
	}
}

func (p *zmPeer) readHexHeader() (zmHeader, error) {
	var digits [14]byte
	for i := range digits {
		c, err := p.readByte()
		if err != nil {
			return zmHeader{}, err
		}
		digits[i] = c & 0x7f
	}
	var raw [7]byte
	if _, err := hex.Decode(raw[:], bytes.ToLower(digits[:])); err != nil {
		return zmHeader{}, errZmBadPacket
	}
	if crc16(0, raw[:5]) != binary.BigEndian.Uint16(raw[5:]) {
		return zmHeader{}, errZmBadPacket
	}
	// Swallow the trailing CR LF if it's already here.
	if c, ok := p.tryReadByte(); ok {
		if c&0x7f == '\r' {
			if c, ok = p.tryReadByte(); ok && c&0x7f != '\n' {
				p.unread(c)
			}
		} else {
			p.unread(c)
		}
	}
	p.last32 = false
	return zmHeader{typ: raw[0], data: [4]byte(raw[1:5])}, nil
}

func (p *zmPeer) readBinHeader(use32 bool) (zmHeader, error) {
	n := 7
	if use32 {
		n = 9
	}
	var raw [9]byte
	for i := 0; i < n; i++ {
		c, end, err := p.readEscaped()
		if err != nil {
			return zmHeader{}, err
		}
		if end {
			return zmHeader{}, errZmBadPacket
		}
		raw[i] = c
	}
	if use32 {
		if crc32.ChecksumIEEE(raw[:5]) != binary.LittleEndian.Uint32(raw[5:9]) {
			return zmHeader{}, errZmBadPacket
		}
	} else if crc16(0, raw[:5]) != binary.BigEndian.Uint16(raw[5:7]) {
		return zmHeader{}, errZmBadPacket
	}
	p.last32 = use32
	return zmHeader{typ: raw[0], data: [4]byte(raw[1:5])}, nil
}

// readData reads one data subpacket and returns it with its terminator.
func (p *zmPeer) readData(limit int) ([]byte, byte, error) {
	data := make([]byte, 0, zmBlock)
	for {
		c, end, err := p.readEscaped()
		if err != nil {
			return nil, 0, err
		}
		if !end {
			if len(data) >= limit {
				return nil, 0, errZmBadPacket
			}
			data = append(data, c)
			continue
		}
		n := 2
		if p.last32 {
			n = 4
		}
		var crc [4]byte
		for i := 0; i < n; i++ {
			b, e, err := p.readEscaped()
			if err != nil {
				return nil, 0, err
			}
			if e {
				return nil, 0, errZmBadPacket
			}
			crc[i] = b
		}
		if p.last32 {
			if crc32.Update(crc32.ChecksumIEEE(data), crc32.IEEETable, []byte{c}) != binary.LittleEndian.Uint32(crc[:]) {
				return nil, 0, errZmBadPacket
			}
		} else if crc16(crc16(0, data), []byte{c}) != binary.BigEndian.Uint16(crc[:2]) {
			return nil, 0, errZmBadPacket
		}
		return data, c, nil
	}
}

// esc appends b to dst with ZDLE escaping.
func (p *zmPeer) esc(dst []byte, b ...byte) []byte {
	for _, c := range b {
		switch {
		case c == zDLE, c == 0x98, c == 0x10, c == 0x90, isFlowCtl(c), c == '\r', c == 0x8d:
			dst = append(dst, zDLE, c^0x40)
		case c == 0x7f:
			dst = append(dst, zDLE, zRUB0)
		case c == 0xff:
			dst = append(dst, zDLE, zRUB1)
		case p.escAll && c&0x60 == 0:
			dst = append(dst, zDLE, c^0x40)
		default:
			dst = append(dst, c)
		}
	}
	return dst
}

func (p *zmPeer) sendHex(h zmHeader) error {
	raw := append([]byte{h.typ}, h.data[:]...)
	raw = binary.BigEndian.AppendUint16(raw, crc16(0, raw))
	out := append([]byte{zPAD, zPAD, zDLE, zHEX}, hex.EncodeToString(raw)...)
	out = append(out, '\r', 0x8a)
	if h.typ != zFIN && h.typ != zACK {
		out = append(out, 0x11) // XON
	}
	return p.write(out)
}

func (p *zmPeer) appendBinHeader(out []byte, h zmHeader, use32 bool) []byte {
	raw := append([]byte{h.typ}, h.data[:]...)
	if use32 {
		out = append(out, zPAD, zDLE, zBIN32)
		raw = binary.LittleEndian.AppendUint32(raw, crc32.ChecksumIEEE(raw))
	} else {
		out = append(out, zPAD, zDLE, zBIN)
		raw = binary.BigEndian.AppendUint16(raw, crc16(0, raw))
	}
	return p.esc(out, raw...)
}

func (p *zmPeer) appendData(out, data []byte, end byte, use32 bool) []byte {
	out = p.esc(out, data...)
	out = append(out, zDLE, end)
	if use32 {
		crc := crc32.Update(crc32.ChecksumIEEE(data), crc32.IEEETable, []byte{end})
		return p.esc(out, binary.LittleEndian.AppendUint32(nil, crc)...)
	}
	crc := crc16(crc16(0, data), []byte{end})
	return p.esc(out, byte(crc>>8), byte(crc))
}

// abort tells the remote sz/rz to stop (8 CAN + 10 backspaces, as lrzsz does).
func (p *zmPeer) abort() {
	_ = p.write(append(bytes.Repeat([]byte{zDLE}, 8), bytes.Repeat([]byte{8}, 10)...))
}

// drainOO consumes the "OO" (over and out) a sender writes after ZFIN.
func (p *zmPeer) drainOO() {
	saved := p.timeout
	p.timeout = 500 * time.Millisecond
	defer func() { p.timeout = saved }()
	for i := 0; i < 2; i++ {
		c, err := p.readByte()
		if err != nil {
			return
		}
		if c != 'O' {
			p.unread(c)
			return
		}
	}
}

// ---- receiving (remote sz) ----

func zmReceive(p *zmPeer, o zmOptions) (saved []string, err error) {
	defer func() {
		if err != nil && !errors.Is(err, errZmRemoteCancel) {
			p.abort()
		}
	}()

	flags := byte(zfCANFDX | zfCANOVIO | zfCANFC32)
	if p.escAll {
		flags |= zfESCCTL
	}
	rinit := flagHeader(zRINIT, flags)
	if err := p.sendHex(rinit); err != nil {
		return nil, err
	}

	retries := 0
	for {
		h, err := p.readHeader()
		if errors.Is(err, errZmTimeout) && retries < zmRetries {
			retries++
			if err := p.sendHex(rinit); err != nil {
				return saved, err
			}
			continue
		}
		if err != nil {
			return saved, err
		}
		retries = 0

		switch h.typ {
		case zRQINIT:
			err = p.sendHex(rinit)
		case zSINIT:
			if _, _, derr := p.readData(zmBlock); derr != nil {
				if !errors.Is(derr, errZmBadPacket) {
					return saved, derr
				}
				err = p.sendHex(zmHeader{typ: zNAK})
			} else {
				err = p.sendHex(zmHeader{typ: zACK})
			}
		case zFILE:
			info, _, derr := p.readData(zmBlock * 4)
			if derr != nil {
				if !errors.Is(derr, errZmBadPacket) {
					return saved, derr
				}
				err = p.sendHex(zmHeader{typ: zNAK})
				break
			}
			name, size := parseZFileInfo(info, o.DecodeName)
			local, ferr := p.receiveFile(name, size, o)
			if ferr != nil {
				return saved, ferr
			}
			saved = append(saved, local)
			err = p.sendHex(rinit)
		case zFIN:
			_ = p.sendHex(zmHeader{typ: zFIN})
			p.drainOO()
			return saved, nil
		case zCOMMAND:
			return saved, errors.New("Zmodem 원격 명령은 지원하지 않습니다")
		default:
			err = p.sendHex(rinit)
		}
		if err != nil {
			return saved, err
		}
	}
}

func parseZFileInfo(info []byte, decode func(string) string) (string, int64) {
	name, rest, _ := bytes.Cut(info, []byte{0})
	var size int64 = -1
	if fields := strings.Fields(string(bytes.TrimRight(rest, "\x00"))); len(fields) > 0 {
		if n, err := strconv.ParseInt(fields[0], 10, 64); err == nil {
			size = n
		}
	}
	s := string(name)
	if decode != nil {
		s = decode(s)
	}
	return sanitizeFileName(s), size
}

// sanitizeFileName keeps only the base name and removes characters Windows rejects.
func sanitizeFileName(name string) string {
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, name)
	name = strings.TrimRight(name, ". ")
	if name == "" || name == "." || name == ".." {
		return "zmodem.bin"
	}
	return name
}

// uniquePath returns dir/name, adding " (n)" before the extension if it exists.
func uniquePath(dir, name string) string {
	p := filepath.Join(dir, name)
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 1; ; i++ {
		if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
			return p
		}
		p = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
	}
}

func (p *zmPeer) receiveFile(name string, size int64, o zmOptions) (string, error) {
	local := uniquePath(o.Dir, name)
	f, err := os.Create(local)
	if err != nil {
		return "", err
	}
	done := false
	defer func() {
		if !done { // failed or cancelled: don't leave a partial file
			f.Close()
			os.Remove(local)
		}
	}()

	var offset int64
	prog := XferProgress{Name: name, Total: max(size, 0)}
	o.Progress(prog)
	if err := p.sendHex(posHeader(zRPOS, 0)); err != nil {
		return "", err
	}

	retries := 0
	resync := func() error {
		if retries++; retries > zmRetries {
			return errZmTimeout
		}
		return p.sendHex(posHeader(zRPOS, offset))
	}

	for {
		h, err := p.readHeader()
		if errors.Is(err, errZmTimeout) {
			if err := resync(); err != nil {
				return "", err
			}
			continue
		}
		if err != nil {
			return "", err
		}

		switch h.typ {
		case zDATA:
			if h.pos() != offset {
				if err := resync(); err != nil {
					return "", err
				}
				continue
			}
			for {
				data, end, err := p.readData(zmMaxPacket)
				if errors.Is(err, errZmBadPacket) || errors.Is(err, errZmTimeout) {
					if err := resync(); err != nil {
						return "", err
					}
					break
				}
				if err != nil {
					return "", err
				}
				retries = 0
				if _, err := f.Write(data); err != nil {
					return "", err
				}
				offset += int64(len(data))
				prog.Done = offset
				o.Progress(prog)
				if end == zCRCW || end == zCRCQ {
					if err := p.sendHex(posHeader(zACK, offset)); err != nil {
						return "", err
					}
				}
				if end == zCRCW || end == zCRCE {
					break
				}
			}
		case zEOF:
			if h.pos() != offset {
				continue // stale EOF while data is still coming
			}
			done = true
			return local, f.Close()
		case zFILE:
			// Our ZRPOS was lost and the sender repeated ZFILE.
			if _, _, err := p.readData(zmBlock * 4); err != nil && !errors.Is(err, errZmBadPacket) {
				return "", err
			}
			if err := resync(); err != nil {
				return "", err
			}
		case zFIN, zABORT, zFERR:
			return "", errZmRemoteCancel
		}
	}
}

// ---- sending (remote rz) ----

func zmSend(p *zmPeer, files []string, o zmOptions) (err error) {
	defer func() {
		if err != nil && !errors.Is(err, errZmRemoteCancel) {
			p.abort()
		}
	}()

	rinit, err := p.waitRINIT()
	if err != nil {
		return err
	}
	use32 := rinit.f0()&zfCANFC32 != 0
	if rinit.f0()&zfESCCTL != 0 {
		p.escAll = true
	}
	window := int64(binary.LittleEndian.Uint16(rinit.data[:2])) // receiver buffer, 0 = unlimited
	if window == 0 || window > zmWindow {
		window = zmWindow
	}

	var left int64
	for _, f := range files {
		if st, err := os.Stat(f); err == nil {
			left += st.Size()
		}
	}
	for i, f := range files {
		sent, err := p.sendFile(f, use32, window, len(files)-i, left, o)
		if err != nil {
			return err
		}
		left -= sent
	}

	for tries := 0; tries < zmRetries; tries++ {
		if err := p.sendHex(zmHeader{typ: zFIN}); err != nil {
			return err
		}
		h, err := p.readHeader()
		if errors.Is(err, errZmTimeout) {
			continue
		}
		if err != nil {
			return err
		}
		if h.typ == zFIN {
			return p.write([]byte("OO"))
		}
	}
	return errZmTimeout
}

func (p *zmPeer) waitRINIT() (zmHeader, error) {
	for tries := 0; tries < zmRetries; {
		h, err := p.readHeader()
		if errors.Is(err, errZmTimeout) {
			tries++
			if err := p.sendHex(zmHeader{typ: zRQINIT}); err != nil {
				return h, err
			}
			continue
		}
		if err != nil {
			return h, err
		}
		if h.typ == zRINIT {
			return h, nil
		}
	}
	return zmHeader{}, errZmTimeout
}

// sendFile offers one file and streams it. Returns the file size.
func (p *zmPeer) sendFile(local string, use32 bool, window int64, filesLeft int, bytesLeft int64, o zmOptions) (int64, error) {
	f, err := os.Open(local)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return 0, err
	}
	size := st.Size()
	name := filepath.Base(local)
	remoteName := name
	if o.EncodeName != nil {
		remoteName = o.EncodeName(name)
	}

	info := fmt.Sprintf("%s\x00%d %o %o 0 %d %d\x00", remoteName, size, st.ModTime().Unix(), 0o644, filesLeft, bytesLeft)
	offer := p.appendBinHeader(nil, flagHeader(zFILE, zcBIN), use32)
	offer = p.appendData(offer, []byte(info), zCRCW, use32)

	prog := XferProgress{Name: name, Total: size, Upload: true}
	o.Progress(prog)

	for tries := 0; ; {
		if err := p.write(offer); err != nil {
			return 0, err
		}
		h, err := p.readHeader()
		if errors.Is(err, errZmTimeout) {
			if tries++; tries > zmRetries {
				return 0, err
			}
			continue
		}
		if err != nil {
			return 0, err
		}
		switch h.typ {
		case zRINIT, zNAK:
			continue // receiver didn't get the offer yet
		case zSKIP:
			return size, nil
		case zCRC:
			// Receiver asks for the file CRC (to decide on resuming).
			crc := crc32.NewIEEE()
			if _, err := io.Copy(crc, f); err != nil {
				return 0, err
			}
			if err := p.sendHex(posHeader(zCRC, int64(crc.Sum32()))); err != nil {
				return 0, err
			}
			h, err = p.readHeader()
			if err != nil {
				return 0, err
			}
			if h.typ == zSKIP {
				return size, nil
			}
			if h.typ != zRPOS {
				continue
			}
			fallthrough
		case zRPOS:
			return size, p.streamFile(f, h.pos(), size, use32, window, prog, o)
		case zFIN, zABORT, zFERR:
			return 0, errZmRemoteCancel
		}
	}
}

func (p *zmPeer) streamFile(f *os.File, pos, size int64, use32 bool, window int64, prog XferProgress, o zmOptions) error {
	buf := make([]byte, zmBlock)
	eofSent := false

	// sendWindow sends data from pos as one frame ending with ZCRCW (ACK expected).
	sendWindow := func() error {
		out := p.appendBinHeader(nil, posHeader(zDATA, pos), use32)
		var sent int64
		for {
			n := int(min(int64(len(buf)), size-pos))
			if _, err := f.ReadAt(buf[:n], pos); err != nil && err != io.EOF {
				return err
			}
			pos += int64(n)
			sent += int64(n)
			end := byte(zCRCG)
			if pos >= size || sent+int64(len(buf)) > window {
				end = zCRCW
			}
			out = p.appendData(out, buf[:n], end, use32)
			if len(out) >= 32*1024 || end == zCRCW {
				if err := p.write(out); err != nil {
					return err
				}
				out = out[:0]
			}
			prog.Done = pos
			o.Progress(prog)
			if end == zCRCW {
				return nil
			}
		}
	}
	sendEOF := func() error {
		eofSent = true
		return p.write(p.appendBinHeader(nil, posHeader(zEOF, size), use32))
	}
	next := func() error {
		if pos >= size {
			return sendEOF()
		}
		eofSent = false
		return sendWindow()
	}

	if err := next(); err != nil {
		return err
	}
	retries := 0
	acked := pos
	for {
		h, err := p.readHeader()
		if errors.Is(err, errZmTimeout) {
			if retries++; retries > zmRetries {
				return err
			}
			if !eofSent {
				pos = acked // the window may have been lost: resend it
			}
			if err := next(); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		switch h.typ {
		case zACK:
			retries = 0
			acked = pos
			if !eofSent {
				if err := next(); err != nil {
					return err
				}
			}
		case zRPOS:
			if retries++; retries > zmRetries {
				return errZmBadPacket
			}
			pos = min(h.pos(), size)
			acked = pos
			if err := next(); err != nil {
				return err
			}
		case zRINIT:
			if eofSent {
				return nil // receiver finished this file
			}
		case zSKIP:
			return nil
		case zFIN, zABORT, zFERR:
			return errZmRemoteCancel
		}
	}
}
