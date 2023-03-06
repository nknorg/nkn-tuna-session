package tests

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
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
	"sync"
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

	err = testUDP(udpConn, uConn)
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

func testUDP(from, to *udp.EncryptUDPConn) error {
	count := 1000
	sendList := make([]string, count)
	recvList := make([]string, count)
	sendNum := 0
	recvNum := 0
	var wg sync.WaitGroup
	var e error
	go func() {
		wg.Add(1)
		receive := make([]byte, 1024)
		for i := 0; i < count; i++ {
			_, _, err := to.ReadFromUDP(receive)
			if err != nil {
				e = err
				return
			}
			recvNum++
			recvList = append(recvList, hex.EncodeToString(receive))
		}
		wg.Done()
	}()

	go func() {
		time.Sleep(1 * time.Second)
		send := make([]byte, 1024)
		wg.Add(1)
		for i := 0; i < count; i++ {
			rand.Read(send)
			_, _, err := from.WriteMsgUDP(send, nil, nil)
			if err != nil {
				e = err
				return
			}
			sendNum++
			sendList = append(sendList, hex.EncodeToString(send))
		}
		wg.Done()
	}()

	wg.Wait()
	if sendNum != recvNum {
		return errors.New("package lost")
	}

	for i := 0; i < sendNum; i++ {
		if sendList[i] != recvList[i] {
			return errors.New("data mismatch")
		}
	}

	return e
}
