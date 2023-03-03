package tests

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"log"
	"math"
	"net"
	"time"

	nkn "github.com/nknorg/nkn-sdk-go"
	ts "github.com/nknorg/nkn-tuna-session"
)

func CreateAccountAndWallet(seedHex string) (acc *nkn.Account, wal *nkn.Wallet, err error) {
	seed, err := hex.DecodeString(seedHex)
	if err != nil {
		log.Fatal(err)
		return nil, nil, err
	}

	acc, err = nkn.NewAccount(seed)
	if err != nil {
		log.Fatal(err)
		return
	}

	wal, err = nkn.NewWallet(acc, nil)
	if err != nil {
		log.Fatal(err)
	}
	bal, _ := wal.Balance()
	fmt.Println("wallet address is ", wal.Address(), "balance is ", bal)

	return
}

func CreateTunaSessionConfig(numListener int) (config *ts.Config) {
	config = &ts.Config{
		NumTunaListeners: numListener,
		TunaMaxPrice:     "0.01",
	}
	return config
}

func CreateDialConfig(timeout int32) (config *nkn.DialConfig) {
	config = &nkn.DialConfig{DialTimeout: timeout}
	return
}

func CreateClientConfig(retries int32) (config *nkn.ClientConfig) {
	config = &nkn.ClientConfig{ConnectRetries: retries}
	return
}

func CreateMultiClient(account *nkn.Account, id string, numClient int) (mc *nkn.MultiClient, err error) {
	clientConfig := CreateClientConfig(1)
	mc, err = nkn.NewMultiClient(account, id, numClient, false, clientConfig)
	if err != nil {
		log.Fatal(err)
	}

	<-mc.OnConnect.C
	return
}

func CreateTunaSession(account *nkn.Account, wallet *nkn.Wallet, mc *nkn.MultiClient, numListener int) (tunaSess *ts.TunaSessionClient, err error) {
	config := CreateTunaSessionConfig(numListener)
	tunaSess, err = ts.NewTunaSessionClient(account, mc, wallet, config)
	if err != nil {
		log.Fatal(err)
	}
	return
}

func read(sess net.Conn) error {
	timeStart := time.Now()

	b := make([]byte, 4)
	n := 0
	for {
		m, err := sess.Read(b[n:])
		if err != nil {
			return err
		}
		n += m
		if n == 4 {
			break
		}
	}

	numBytes := int(binary.LittleEndian.Uint32(b))

	b = make([]byte, 1024)
	bytesReceived := 0
	for {
		n, err := sess.Read(b)
		if err != nil {
			return err
		}
		for i := 0; i < n; i++ {
			if b[i] != byte(bytesReceived%256) {
				return fmt.Errorf("byte %d should be %d, got %d", bytesReceived, bytesReceived%256, b[i])
			}
			bytesReceived++
		}
		if ((bytesReceived - n) * 10 / numBytes) != (bytesReceived * 10 / numBytes) {
			log.Println("Received", bytesReceived, "bytes", float64(bytesReceived)/math.Pow(2, 20)/(float64(time.Since(timeStart))/float64(time.Second)), "MB/s")
		}
		if bytesReceived == numBytes {
			log.Println("Finish receiving", bytesReceived, "bytes")
			return nil
		}
	}
}

func write(sess net.Conn, numBytes int) error {
	timeStart := time.Now()

	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, uint32(numBytes))
	_, err := sess.Write(b)
	if err != nil {
		return err
	}

	bytesSent := 0
	for i := 0; i < numBytes/1024; i++ {
		b := make([]byte, 1024)
		for j := 0; j < len(b); j++ {
			b[j] = byte(bytesSent % 256)
			bytesSent++
		}
		n, err := sess.Write(b)
		if err != nil {
			return err
		}
		if n != len(b) {
			return fmt.Errorf("sent %d instead of %d bytes", n, len(b))
		}

		if bytesSent == numBytes {
			log.Println("Finish sending", bytesSent, "bytes", float64(bytesSent)/math.Pow(2, 20)/(float64(time.Since(timeStart))/float64(time.Second)), "MB/s")
			break
		} else {
			if ((bytesSent - n) * 10 / numBytes) != (bytesSent * 10 / numBytes) {
				log.Println("Sent", bytesSent, "bytes", float64(bytesSent)/math.Pow(2, 20)/(float64(time.Since(timeStart))/float64(time.Second)), "MB/s")
				// slow down for testing disconnect and reconnect
				time.Sleep(2 * time.Second)
			}
		}
	}
	return nil
}
