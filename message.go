package session

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"

	"github.com/nknorg/nkn/v2/crypto/ed25519"
	"golang.org/x/crypto/nacl/box"
)

const (
	nonceSize     = 24
	sharedKeySize = 32
)

type Request struct {
	Action string `json:"action"`
}

type PubAddr struct {
	IP   string `json:"ip"`
	Port uint32 `json:"port"`
}

type PubAddrs struct {
	Addrs []PubAddr `json:"addrs"`
}

func (c *TunaSessionClient) getOrComputeSharedKey(remotePublicKey []byte) (*[sharedKeySize]byte, error) {
	c.RLock()
	sharedKey, ok := c.sharedKeys[string(remotePublicKey)]
	c.RUnlock()
	if ok && sharedKey != nil {
		return sharedKey, nil
	}

	if len(remotePublicKey) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("public key length is %d, expecting %d", len(remotePublicKey), ed25519.PublicKeySize)
	}

	var pk [ed25519.PublicKeySize]byte
	copy(pk[:], remotePublicKey)
	curve25519PublicKey, ok := ed25519.PublicKeyToCurve25519PublicKey(&pk)
	if !ok {
		return nil, fmt.Errorf("converting public key %x to curve25519 public key failed", remotePublicKey)
	}

	var sk [ed25519.PrivateKeySize]byte
	copy(sk[:], c.clientAccount.PrivKey())
	curveSecretKey := ed25519.PrivateKeyToCurve25519PrivateKey(&sk)

	sharedKey = new([sharedKeySize]byte)
	box.Precompute(sharedKey, curve25519PublicKey, curveSecretKey)

	c.Lock()
	c.sharedKeys[string(remotePublicKey)] = sharedKey
	c.Unlock()

	return sharedKey, nil
}

func encrypt(message []byte, sharedKey *[sharedKeySize]byte) ([]byte, []byte, error) {
	encrypted := make([]byte, len(message)+box.Overhead)
	var nonce [nonceSize]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, nil, err
	}
	box.SealAfterPrecomputation(encrypted[:0], message, &nonce, sharedKey)
	return encrypted, nonce[:], nil
}

func decrypt(message []byte, nonce [nonceSize]byte, sharedKey *[sharedKeySize]byte) ([]byte, error) {
	decrypted := make([]byte, len(message)-box.Overhead)
	_, ok := box.OpenAfterPrecomputation(decrypted[:0], message, &nonce, sharedKey)
	if !ok {
		return nil, errors.New("decrypt message failed")
	}

	return decrypted, nil
}

func writeMessage(conn net.Conn, buf []byte) error {
	msgSizeBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(msgSizeBuf, uint32(len(buf)))
	_, err := conn.Write(msgSizeBuf)
	if err != nil {
		return err
	}

	_, err = conn.Write(buf)
	return err
}

func readFull(conn net.Conn, buf []byte) error {
	bytesRead := 0
	for {
		n, err := conn.Read(buf[bytesRead:])
		if err != nil {
			return err
		}
		bytesRead += n
		if bytesRead == len(buf) {
			return nil
		}
	}
}

func readMessage(conn net.Conn) ([]byte, error) {
	msgSizeBuf := make([]byte, 4)
	err := readFull(conn, msgSizeBuf)
	if err != nil {
		return nil, err
	}

	msgSize := binary.LittleEndian.Uint32(msgSizeBuf)
	buf := make([]byte, msgSize)
	err = readFull(conn, buf)
	if err != nil {
		return nil, err
	}

	return buf, nil
}
