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
	"github.com/nknorg/nkn/v2/vault"
	"github.com/nknorg/tuna"
	"github.com/nknorg/tuna/pb"
	_ "github.com/nknorg/tuna/tests"
	"github.com/nknorg/tuna/types"
	"github.com/nknorg/tuna/util"
	"io"
	"net"
	"os"
	"strconv"
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

	// Set up tuna
	tunaPubKey, tunaPrivKey, _ := crypto.GenKeyPair()
	tunaSeed := crypto.GetSeedFromPrivateKey(tunaPrivKey)
	go runReverseEntry(tunaSeed)
	time.Sleep(15 * time.Second)

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
		TunaMaxPrice:         "0.0",
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

	n := &types.Node{
		Delay:     0,
		Bandwidth: 0,
		Metadata: &pb.ServiceMetadata{
			Ip:              "127.0.0.1",
			TcpPort:         30020,
			UdpPort:         30021,
			ServiceId:       0,
			Price:           "0.0",
			BeneficiaryAddr: "",
		},
		Address:     hex.EncodeToString(tunaPubKey),
		MetadataRaw: "CgkxMjcuMC4wLjEQxOoBGMXqAToFMC4wMDE=",
	}
	c.SetTunaNode(n)

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

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := dialID + strconv.Itoa(i)
			mm, err := nkn.NewMultiClient(account2, id, 4, false, clientConfig)
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
		}(i)
	}
	wg.Wait()
}

func testTCP(conn net.Conn) error {
	send := make([]byte, 4096)
	receive := make([]byte, 4096)

	for i := 0; i < 1000; i++ {
		rand.Read(send)
		conn.Write(send)
		io.ReadFull(conn, receive)
		if !bytes.Equal(send, receive) {
			return errors.New("bytes not equal")
		}
	}
	return nil
}

func testUDP(from, to *tuna.EncryptUDPConn) error {
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

func runReverseEntry(seed []byte) error {
	entryAccount, err := vault.NewAccountWithSeed(seed)
	if err != nil {
		return err
	}
	seedRPCServerAddr := nkn.NewStringArray(nkn.DefaultSeedRPCServerAddr...)

	walletConfig := &nkn.WalletConfig{
		SeedRPCServerAddr: seedRPCServerAddr,
	}
	entryWallet, err := nkn.NewWallet(&nkn.Account{Account: entryAccount}, walletConfig)
	if err != nil {
		return err
	}
	entryConfig := new(tuna.EntryConfiguration)
	err = util.ReadJSON("config.reverse.entry.json", entryConfig)
	if err != nil {
		return err
	}
	err = tuna.StartReverse(entryConfig, entryWallet)
	if err != nil {
		return err
	}
	select {}
}
