package tests

import (
	"bytes"
	"crypto/rand"
	"errors"
	"github.com/nknorg/ncp-go"
	"github.com/nknorg/nkn-sdk-go"
	ts "github.com/nknorg/nkn-tuna-session"
	"github.com/nknorg/nkn/v2/crypto"
	_ "github.com/nknorg/tuna/tests"
	"github.com/nknorg/tuna/udp"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

const (
	dialID   = "alice"
	listenID = "bob"
)

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}

func TestTunaSession(t *testing.T) {
	_, privKey, _ := crypto.GenKeyPair()
	seed := crypto.GetSeedFromPrivateKey(privKey)
	_, privKey2, _ := crypto.GenKeyPair()
	seed2 := crypto.GetSeedFromPrivateKey(privKey2)

	account, err := nkn.NewAccount(seed)
	if err != nil {
		t.Fatal(err)
	}
	account2, err := nkn.NewAccount(seed2)
	if err != nil {
		t.Fatal(err)
	}
	seedRPCServerAddr := nkn.NewStringArray(nkn.DefaultSeedRPCServerAddr...)
	walletConfig := &nkn.WalletConfig{
		SeedRPCServerAddr: seedRPCServerAddr,
	}

	wallet, err := nkn.NewWallet(account, walletConfig)
	if err != nil {
		t.Fatal(err)
	}
	wallet2, err := nkn.NewWallet(account2, walletConfig)
	if err != nil {
		t.Fatal(err)
	}
	clientConfig := &nkn.ClientConfig{ConnectRetries: 1}
	dialConfig := &nkn.DialConfig{DialTimeout: 5000}
	config := &ts.Config{
		NumTunaListeners:     1,
		TunaMeasureBandwidth: false,
		TunaMaxPrice:         "0.01",
		SessionConfig:        &ncp.Config{MTU: int32(0)},
	}

	m, err := nkn.NewMultiClient(account, listenID, 4, false, clientConfig)
	if err != nil {
		t.Fatal(err)
	}

	<-m.OnConnect.C

	c, err := ts.NewTunaSessionClient(account, m, wallet, config)
	if err != nil {
		t.Fatal(err)
	}

	err = c.Listen(nil)
	if err != nil {
		t.Fatal(err)
	}

	uConn, err := c.ListenUDP(nil)
	if err != nil {
		t.Fatal(err)
	}

	t.Log("Listening at", c.Addr())

	go func() {
		for {
			conn, err := c.Accept()
			if err != nil {
				t.Fatal(err)
			}
			go func(conn net.Conn) {
				defer func() {
					conn.Close()
				}()
				io.Copy(conn, conn)
			}(conn)
		}
	}()

	go func() {
		buffer := make([]byte, 65536)
		for {
			n, addr, err := uConn.ReadFromUDP(buffer)
			if err != nil {
				t.Fatal(err)
			}
			n, _, err = uConn.WriteMsgUDP(buffer[:n], nil, addr)
			if err != nil {
				t.Fatal(err)
			}
		}
	}()

	mm, err := nkn.NewMultiClient(account2, dialID, 4, false, clientConfig)
	if err != nil {
		t.Fatal(err)
	}

	<-mm.OnConnect.C
	time.Sleep(5 * time.Second)

	cc, err := ts.NewTunaSessionClient(account2, mm, wallet2, config)
	if err != nil {
		t.Fatal(err)
	}

	dialAddr := listenID + "." + strings.SplitN(c.Addr().String(), ".", 2)[1]

	t.Log("Dial to", dialAddr)

	s, err := cc.DialWithConfig(dialAddr, dialConfig)
	if err != nil {
		t.Fatal(err)
	}

	err = testTCP(s)
	if err != nil {
		t.Fatal(err)
	}

	udpConn, err := cc.DialUDPWithConfig(dialAddr, dialConfig)
	if err != nil {
		t.Fatal(err)
	}

	err = testUDP(udpConn)
	if err != nil {
		t.Fatal(err)
	}
}

func testTCP(conn net.Conn) error {
	send := make([]byte, 4096)
	receive := make([]byte, 4096)

	for i := 0; i < 10; i++ {
		rand.Read(send)
		conn.Write(send)
		io.ReadFull(conn, receive)
		if !bytes.Equal(send, receive) {
			return errors.New("bytes not equal")
		}
	}
	return nil
}

func testUDP(conn *udp.EncryptUDPConn) error {
	send := make([]byte, 4096)
	receive := make([]byte, 4096)
	for i := 0; i < 10; i++ {
		rand.Read(send)
		conn.WriteMsgUDP(send, nil, nil)
		conn.ReadFromUDP(receive)
		if !bytes.Equal(send, receive) {
			return errors.New("bytes not equal")
		}
	}
	return nil
}
