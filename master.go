package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"errors"
	"io"
	"unsafe"

	xor "github.com/templexxx/xorsimd"
	"golang.org/x/crypto/pbkdf2"
)

var (
	ErrInvalidKeyId = errors.New("invalid key id")
)
var (
	IV          = []byte{167, 79, 156, 18, 172, 27, 1, 164, 21, 242, 193, 252, 120, 230, 107, 115}
	SALT        = "safebox"
	PBKDF2_ITER = 4096
)

const MasterKeyLength = 256 * 1024 // 256 KB
type MasterKey struct {
	masterKey [MasterKeyLength]byte
}

type DerivedKey [24]byte

func newMasterKey() *MasterKey {
	mkey := new(MasterKey)
	return mkey
}

func (mkey *MasterKey) generateMasterKey() error {
	_, err := io.ReadFull(rand.Reader, mkey.masterKey[:])
	if err != nil {
		return err
	}
	return nil
}

// derive the N-th id with current master key with specified key size
func (mkey *MasterKey) deriveKey(id int, keySize int) (key []byte, err error) {
	if id >= MasterKeyLength/aes.BlockSize || id < 0 {
		return nil, ErrInvalidKeyId
	}

	// Approcach:
	// 1. take the N-th 16Byte as the key to encrypt the whole master key in CFB
	var encryptedKey [MasterKeyLength]byte
	aesBlock, err := NewAESBlockCrypt(mkey.masterKey[id*aes.BlockSize : (id+1)*aes.BlockSize])
	if err != nil {
		return nil, err
	}
	aesBlock.Encrypt(encryptedKey[:], mkey.masterKey[:])

	// 2. create a SHA-256 of the encrypted master key as the derived key
	md := sha256.Sum256(encryptedKey[:])

	// 3. use pbkdf2 to suit the key size
	key = pbkdf2.Key(md[:], []byte(SALT), PBKDF2_ITER, keySize, sha1.New)
	return key, err
}

// BlockCrypt defines encryption/decryption methods for a given byte slice.
// Notes on implementing: the data to be encrypted contains a builtin
// nonce at the first 16 bytes
type BlockCrypt interface {
	// Encrypt encrypts the whole block in src into dst.
	// Dst and src may point at the same memory.
	Encrypt(dst, src []byte)

	// Decrypt decrypts the whole block in src into dst.
	// Dst and src may point at the same memory.
	Decrypt(dst, src []byte)
}

type aesBlockCrypt struct {
	encbuf [aes.BlockSize]byte
	decbuf [2 * aes.BlockSize]byte
	block  cipher.Block
}

// NewAESBlockCrypt https://en.wikipedia.org/wiki/Advanced_Encryption_Standard
func NewAESBlockCrypt(key []byte) (BlockCrypt, error) {
	c := new(aesBlockCrypt)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	c.block = block
	return c, nil
}

func (c *aesBlockCrypt) Encrypt(dst, src []byte) { encrypt(c.block, dst, src, c.encbuf[:]) }
func (c *aesBlockCrypt) Decrypt(dst, src []byte) { decrypt(c.block, dst, src, c.decbuf[:]) }

func encrypt(block cipher.Block, dst, src, buf []byte) {
	switch block.BlockSize() {
	case 8:
		encrypt8(block, dst, src, buf)
	case 16:
		encrypt16(block, dst, src, buf)
	default:
		panic("unsupported cipher block size")
	}
}

// optimized encryption for the ciphers which works in 8-bytes
func encrypt8(block cipher.Block, dst, src, buf []byte) {
	tbl := buf[:8]
	block.Encrypt(tbl, IV)
	n := len(src) / 8
	base := 0
	repeat := n / 8
	left := n % 8
	ptr_tbl := (*uint64)(unsafe.Pointer(&tbl[0]))

	for i := 0; i < repeat; i++ {
		s := src[base:][0:64]
		d := dst[base:][0:64]
		// 1
		*(*uint64)(unsafe.Pointer(&d[0])) = *(*uint64)(unsafe.Pointer(&s[0])) ^ *ptr_tbl
		block.Encrypt(tbl, d[0:8])
		// 2
		*(*uint64)(unsafe.Pointer(&d[8])) = *(*uint64)(unsafe.Pointer(&s[8])) ^ *ptr_tbl
		block.Encrypt(tbl, d[8:16])
		// 3
		*(*uint64)(unsafe.Pointer(&d[16])) = *(*uint64)(unsafe.Pointer(&s[16])) ^ *ptr_tbl
		block.Encrypt(tbl, d[16:24])
		// 4
		*(*uint64)(unsafe.Pointer(&d[24])) = *(*uint64)(unsafe.Pointer(&s[24])) ^ *ptr_tbl
		block.Encrypt(tbl, d[24:32])
		// 5
		*(*uint64)(unsafe.Pointer(&d[32])) = *(*uint64)(unsafe.Pointer(&s[32])) ^ *ptr_tbl
		block.Encrypt(tbl, d[32:40])
		// 6
		*(*uint64)(unsafe.Pointer(&d[40])) = *(*uint64)(unsafe.Pointer(&s[40])) ^ *ptr_tbl
		block.Encrypt(tbl, d[40:48])
		// 7
		*(*uint64)(unsafe.Pointer(&d[48])) = *(*uint64)(unsafe.Pointer(&s[48])) ^ *ptr_tbl
		block.Encrypt(tbl, d[48:56])
		// 8
		*(*uint64)(unsafe.Pointer(&d[56])) = *(*uint64)(unsafe.Pointer(&s[56])) ^ *ptr_tbl
		block.Encrypt(tbl, d[56:64])
		base += 64
	}

	switch left {
	case 7:
		*(*uint64)(unsafe.Pointer(&dst[base])) = *(*uint64)(unsafe.Pointer(&src[base])) ^ *ptr_tbl
		block.Encrypt(tbl, dst[base:])
		base += 8
		fallthrough
	case 6:
		*(*uint64)(unsafe.Pointer(&dst[base])) = *(*uint64)(unsafe.Pointer(&src[base])) ^ *ptr_tbl
		block.Encrypt(tbl, dst[base:])
		base += 8
		fallthrough
	case 5:
		*(*uint64)(unsafe.Pointer(&dst[base])) = *(*uint64)(unsafe.Pointer(&src[base])) ^ *ptr_tbl
		block.Encrypt(tbl, dst[base:])
		base += 8
		fallthrough
	case 4:
		*(*uint64)(unsafe.Pointer(&dst[base])) = *(*uint64)(unsafe.Pointer(&src[base])) ^ *ptr_tbl
		block.Encrypt(tbl, dst[base:])
		base += 8
		fallthrough
	case 3:
		*(*uint64)(unsafe.Pointer(&dst[base])) = *(*uint64)(unsafe.Pointer(&src[base])) ^ *ptr_tbl
		block.Encrypt(tbl, dst[base:])
		base += 8
		fallthrough
	case 2:
		*(*uint64)(unsafe.Pointer(&dst[base])) = *(*uint64)(unsafe.Pointer(&src[base])) ^ *ptr_tbl
		block.Encrypt(tbl, dst[base:])
		base += 8
		fallthrough
	case 1:
		*(*uint64)(unsafe.Pointer(&dst[base])) = *(*uint64)(unsafe.Pointer(&src[base])) ^ *ptr_tbl
		block.Encrypt(tbl, dst[base:])
		base += 8
		fallthrough
	case 0:
		xorBytes(dst[base:], src[base:], tbl)
	}
}

// optimized encryption for the ciphers which works in 16-bytes
func encrypt16(block cipher.Block, dst, src, buf []byte) {
	tbl := buf[:16]
	block.Encrypt(tbl, IV)
	n := len(src) / 16
	base := 0
	repeat := n / 8
	left := n % 8
	for i := 0; i < repeat; i++ {
		s := src[base:][0:128]
		d := dst[base:][0:128]
		// 1
		xor.Bytes16Align(d[0:16], s[0:16], tbl)
		block.Encrypt(tbl, d[0:16])
		// 2
		xor.Bytes16Align(d[16:32], s[16:32], tbl)
		block.Encrypt(tbl, d[16:32])
		// 3
		xor.Bytes16Align(d[32:48], s[32:48], tbl)
		block.Encrypt(tbl, d[32:48])
		// 4
		xor.Bytes16Align(d[48:64], s[48:64], tbl)
		block.Encrypt(tbl, d[48:64])
		// 5
		xor.Bytes16Align(d[64:80], s[64:80], tbl)
		block.Encrypt(tbl, d[64:80])
		// 6
		xor.Bytes16Align(d[80:96], s[80:96], tbl)
		block.Encrypt(tbl, d[80:96])
		// 7
		xor.Bytes16Align(d[96:112], s[96:112], tbl)
		block.Encrypt(tbl, d[96:112])
		// 8
		xor.Bytes16Align(d[112:128], s[112:128], tbl)
		block.Encrypt(tbl, d[112:128])
		base += 128
	}

	switch left {
	case 7:
		xor.Bytes16Align(dst[base:], src[base:], tbl)
		block.Encrypt(tbl, dst[base:])
		base += 16
		fallthrough
	case 6:
		xor.Bytes16Align(dst[base:], src[base:], tbl)
		block.Encrypt(tbl, dst[base:])
		base += 16
		fallthrough
	case 5:
		xor.Bytes16Align(dst[base:], src[base:], tbl)
		block.Encrypt(tbl, dst[base:])
		base += 16
		fallthrough
	case 4:
		xor.Bytes16Align(dst[base:], src[base:], tbl)
		block.Encrypt(tbl, dst[base:])
		base += 16
		fallthrough
	case 3:
		xor.Bytes16Align(dst[base:], src[base:], tbl)
		block.Encrypt(tbl, dst[base:])
		base += 16
		fallthrough
	case 2:
		xor.Bytes16Align(dst[base:], src[base:], tbl)
		block.Encrypt(tbl, dst[base:])
		base += 16
		fallthrough
	case 1:
		xor.Bytes16Align(dst[base:], src[base:], tbl)
		block.Encrypt(tbl, dst[base:])
		base += 16
		fallthrough
	case 0:
		xorBytes(dst[base:], src[base:], tbl)
	}
}

// decryption
func decrypt(block cipher.Block, dst, src, buf []byte) {
	switch block.BlockSize() {
	case 8:
		decrypt8(block, dst, src, buf)
	case 16:
		decrypt16(block, dst, src, buf)
	default:
		panic("unsupported cipher block size")
	}
}

// decrypt 8 bytes block, all byte slices are supposed to be 64bit aligned
func decrypt8(block cipher.Block, dst, src, buf []byte) {
	tbl := buf[0:8]
	next := buf[8:16]
	block.Encrypt(tbl, IV)
	n := len(src) / 8
	base := 0
	repeat := n / 8
	left := n % 8
	ptr_tbl := (*uint64)(unsafe.Pointer(&tbl[0]))
	ptr_next := (*uint64)(unsafe.Pointer(&next[0]))

	for i := 0; i < repeat; i++ {
		s := src[base:][0:64]
		d := dst[base:][0:64]
		// 1
		block.Encrypt(next, s[0:8])
		*(*uint64)(unsafe.Pointer(&d[0])) = *(*uint64)(unsafe.Pointer(&s[0])) ^ *ptr_tbl
		// 2
		block.Encrypt(tbl, s[8:16])
		*(*uint64)(unsafe.Pointer(&d[8])) = *(*uint64)(unsafe.Pointer(&s[8])) ^ *ptr_next
		// 3
		block.Encrypt(next, s[16:24])
		*(*uint64)(unsafe.Pointer(&d[16])) = *(*uint64)(unsafe.Pointer(&s[16])) ^ *ptr_tbl
		// 4
		block.Encrypt(tbl, s[24:32])
		*(*uint64)(unsafe.Pointer(&d[24])) = *(*uint64)(unsafe.Pointer(&s[24])) ^ *ptr_next
		// 5
		block.Encrypt(next, s[32:40])
		*(*uint64)(unsafe.Pointer(&d[32])) = *(*uint64)(unsafe.Pointer(&s[32])) ^ *ptr_tbl
		// 6
		block.Encrypt(tbl, s[40:48])
		*(*uint64)(unsafe.Pointer(&d[40])) = *(*uint64)(unsafe.Pointer(&s[40])) ^ *ptr_next
		// 7
		block.Encrypt(next, s[48:56])
		*(*uint64)(unsafe.Pointer(&d[48])) = *(*uint64)(unsafe.Pointer(&s[48])) ^ *ptr_tbl
		// 8
		block.Encrypt(tbl, s[56:64])
		*(*uint64)(unsafe.Pointer(&d[56])) = *(*uint64)(unsafe.Pointer(&s[56])) ^ *ptr_next
		base += 64
	}

	switch left {
	case 7:
		block.Encrypt(next, src[base:])
		*(*uint64)(unsafe.Pointer(&dst[base])) = *(*uint64)(unsafe.Pointer(&src[base])) ^ *(*uint64)(unsafe.Pointer(&tbl[0]))
		tbl, next = next, tbl
		base += 8
		fallthrough
	case 6:
		block.Encrypt(next, src[base:])
		*(*uint64)(unsafe.Pointer(&dst[base])) = *(*uint64)(unsafe.Pointer(&src[base])) ^ *(*uint64)(unsafe.Pointer(&tbl[0]))
		tbl, next = next, tbl
		base += 8
		fallthrough
	case 5:
		block.Encrypt(next, src[base:])
		*(*uint64)(unsafe.Pointer(&dst[base])) = *(*uint64)(unsafe.Pointer(&src[base])) ^ *(*uint64)(unsafe.Pointer(&tbl[0]))
		tbl, next = next, tbl
		base += 8
		fallthrough
	case 4:
		block.Encrypt(next, src[base:])
		*(*uint64)(unsafe.Pointer(&dst[base])) = *(*uint64)(unsafe.Pointer(&src[base])) ^ *(*uint64)(unsafe.Pointer(&tbl[0]))
		tbl, next = next, tbl
		base += 8
		fallthrough
	case 3:
		block.Encrypt(next, src[base:])
		*(*uint64)(unsafe.Pointer(&dst[base])) = *(*uint64)(unsafe.Pointer(&src[base])) ^ *(*uint64)(unsafe.Pointer(&tbl[0]))
		tbl, next = next, tbl
		base += 8
		fallthrough
	case 2:
		block.Encrypt(next, src[base:])
		*(*uint64)(unsafe.Pointer(&dst[base])) = *(*uint64)(unsafe.Pointer(&src[base])) ^ *(*uint64)(unsafe.Pointer(&tbl[0]))
		tbl, next = next, tbl
		base += 8
		fallthrough
	case 1:
		block.Encrypt(next, src[base:])
		*(*uint64)(unsafe.Pointer(&dst[base])) = *(*uint64)(unsafe.Pointer(&src[base])) ^ *(*uint64)(unsafe.Pointer(&tbl[0]))
		tbl, next = next, tbl
		base += 8
		fallthrough
	case 0:
		xorBytes(dst[base:], src[base:], tbl)
	}
}

func decrypt16(block cipher.Block, dst, src, buf []byte) {
	tbl := buf[0:16]
	next := buf[16:32]
	block.Encrypt(tbl, IV)
	n := len(src) / 16
	base := 0
	repeat := n / 8
	left := n % 8
	for i := 0; i < repeat; i++ {
		s := src[base:][0:128]
		d := dst[base:][0:128]
		// 1
		block.Encrypt(next, s[0:16])
		xor.Bytes16Align(d[0:16], s[0:16], tbl)
		// 2
		block.Encrypt(tbl, s[16:32])
		xor.Bytes16Align(d[16:32], s[16:32], next)
		// 3
		block.Encrypt(next, s[32:48])
		xor.Bytes16Align(d[32:48], s[32:48], tbl)
		// 4
		block.Encrypt(tbl, s[48:64])
		xor.Bytes16Align(d[48:64], s[48:64], next)
		// 5
		block.Encrypt(next, s[64:80])
		xor.Bytes16Align(d[64:80], s[64:80], tbl)
		// 6
		block.Encrypt(tbl, s[80:96])
		xor.Bytes16Align(d[80:96], s[80:96], next)
		// 7
		block.Encrypt(next, s[96:112])
		xor.Bytes16Align(d[96:112], s[96:112], tbl)
		// 8
		block.Encrypt(tbl, s[112:128])
		xor.Bytes16Align(d[112:128], s[112:128], next)
		base += 128
	}

	switch left {
	case 7:
		block.Encrypt(next, src[base:])
		xor.Bytes16Align(dst[base:], src[base:], tbl)
		tbl, next = next, tbl
		base += 16
		fallthrough
	case 6:
		block.Encrypt(next, src[base:])
		xor.Bytes16Align(dst[base:], src[base:], tbl)
		tbl, next = next, tbl
		base += 16
		fallthrough
	case 5:
		block.Encrypt(next, src[base:])
		xor.Bytes16Align(dst[base:], src[base:], tbl)
		tbl, next = next, tbl
		base += 16
		fallthrough
	case 4:
		block.Encrypt(next, src[base:])
		xor.Bytes16Align(dst[base:], src[base:], tbl)
		tbl, next = next, tbl
		base += 16
		fallthrough
	case 3:
		block.Encrypt(next, src[base:])
		xor.Bytes16Align(dst[base:], src[base:], tbl)
		tbl, next = next, tbl
		base += 16
		fallthrough
	case 2:
		block.Encrypt(next, src[base:])
		xor.Bytes16Align(dst[base:], src[base:], tbl)
		tbl, next = next, tbl
		base += 16
		fallthrough
	case 1:
		block.Encrypt(next, src[base:])
		xor.Bytes16Align(dst[base:], src[base:], tbl)
		tbl, next = next, tbl
		base += 16
		fallthrough
	case 0:
		xorBytes(dst[base:], src[base:], tbl)
	}
}

// per bytes xors
func xorBytes(dst, a, b []byte) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	if n == 0 {
		return 0
	}

	for i := 0; i < n; i++ {
		dst[i] = a[i] ^ b[i]
	}
	return n
}
