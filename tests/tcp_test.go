package tests

import (
	"encoding/binary"
	"fmt"
	"log"
	"math"
	"net"
	"testing"
	"time"

	"github.com/nknorg/ncp-go"
	ts "github.com/nknorg/nkn-tuna-session"
)

// status of listener and dialer
const (
	listening = string("listening")
	accepted  = string("accepted")
	dialed    = string("dialed")
	end       = string("end")
)

// sync listener and dialer
func waitfor(ch chan string, result string) {
	for {
		str := <-ch
		if str == result {
			break
		}
	}
}

// go test -v -run=TestNormalListener
func TestNormalListener(t *testing.T) {
	ch := make(chan string, 1)

	go func() {
		StartTunaTcpListener(numTcpListener, ch)
	}()

	waitfor(ch, end)
}

// go test -v -run=TestNormalDialer
func TestNormalDialer(t *testing.T) {
	ch := make(chan string, 1)

	go func() {
		// wait for Listener be ready
		time.Sleep(2 * time.Second)
		StartTunaTcpDialer(bytesToSend, numTcpListener, ch)
	}()

	waitfor(ch, end)
}

// go test -v -run=TestCloseOneConnListener
func TestCloseOneConnListener(t *testing.T) {
	var tunaSess *ts.TunaSessionClient
	var ncpSess *ncp.Session
	ch := make(chan string, 1)

	go func() {
		tunaSess, ncpSess = StartTunaTcpListener(numTcpListener, ch)
	}()

	waitfor(ch, accepted)
	time.Sleep(3 * time.Second)
	tunaSess.CloseOneConn(ncpSess, "2")

	waitfor(ch, end)
}

// go test -v -run=TestCloseOneConnDialer
func TestCloseOneConnDialer(t *testing.T) {
	var tunaSess *ts.TunaSessionClient
	var ncpSess *ncp.Session
	ch := make(chan string, 1)

	go func() {
		// wait for Listener be ready
		time.Sleep(2 * time.Second)
		tunaSess, ncpSess = StartTunaTcpDialer(bytesToSend, numTcpListener, ch)
	}()

	waitfor(ch, dialed)
	time.Sleep(2 * time.Second)
	tunaSess.CloseOneConn(ncpSess, "1")

	waitfor(ch, end)
}

// go test -v -run=TestCloseAllConnDialer
func TestCloseAllConnDialer(t *testing.T) {
	var tunaSess *ts.TunaSessionClient
	var ncpSess *ncp.Session
	ch := make(chan string, 1)

	go func() {
		// wait for Listener be ready
		time.Sleep(2 * time.Second)
		tunaSess, ncpSess = StartTunaTcpDialer(bytesToSend, numTcpListener, ch)
	}()

	waitfor(ch, dialed)
	time.Sleep(2 * time.Second)
	tunaSess.CloseOneConn(ncpSess, "0")
	time.Sleep(2 * time.Second)
	tunaSess.CloseOneConn(ncpSess, "1")
	time.Sleep(2 * time.Second)
	tunaSess.CloseOneConn(ncpSess, "2")
	time.Sleep(2 * time.Second)
	tunaSess.CloseOneConn(ncpSess, "3")

	waitfor(ch, end)
}

func readTcp(sess net.Conn) error {
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

func StartTunaTcpListener(numListener int, ch chan string) (tunaSess *ts.TunaSessionClient, ncpSess *ncp.Session) {
	acc, wal, err := CreateAccountAndWallet(seedHex)
	if err != nil {
		log.Fatal("CreateAccountAndWallet err: ", err)
	}
	mc, err := CreateMultiClient(acc, listenerId, 2)
	if err != nil {
		log.Fatal("CreateMultiClient err: ", err)
	}
	tunaSess, err = CreateTunaSession(acc, wal, mc, numListener)
	if err != nil {
		log.Fatal("CreateTunaSession err: ", err)
	}

	err = tunaSess.Listen(nil)
	if err != nil {
		log.Fatal("tunaSess.Listen ", err)
	}
	log.Printf("tcp listener is listening...")
	ch <- listening

	go func() {
		for {
			sess, err := tunaSess.Accept()
			if err != nil {
				log.Fatal("tunaSess.Accept ", err)
			}
			ncpSess = sess.(*ncp.Session)
			log.Printf("tcp listener accepted a new connection...")
			ch <- accepted

			go func() {
				err = readTcp(ncpSess)
				if err != nil {
					log.Printf("StartTunaListner read err:%v\n", err)
				} else {
					log.Printf("Finished reading, close ncp.session now\n")
				}
				ncpSess.Close()

				log.Printf("tcp listener finish job, exit now...")
				ch <- end
			}()
		}
	}()

	return
}

func StartTunaTcpDialer(numBytes int, numListener int, ch chan string) (tunaSess *ts.TunaSessionClient, ncpSess *ncp.Session) {
	acc, wal, err := CreateAccountAndWallet(seedHex)
	if err != nil {
		log.Fatal("CreateAccountAndWallet err: ", err)
	}
	mc, err := CreateMultiClient(acc, dialerId, 2)
	if err != nil {
		log.Fatal("CreateMultiClient err: ", err)
	}

	tunaSess, err = CreateTunaSession(acc, wal, mc, numListener)
	if err != nil {
		log.Fatal("CreateTunaSession err: ", err)
	}

	diaConfig := CreateDialConfig(5000)
	ncpSess, err = tunaSess.DialWithConfig(remoteAddr, diaConfig)
	if err != nil {
		log.Fatal("tunaSess.DialWithConfig ", err)
	}
	ch <- dialed

	go func() {
		err = writeTcp(ncpSess, numBytes)
		if err != nil {
			log.Printf("StartTunaDialer write err:%v\n", err)
		} else {
			log.Printf("Finished writing, close ncp.session now\n")
		}
		time.Sleep(time.Second) // wait for reader to read data.
		ncpSess.Close()
		ch <- end
	}()

	return
}

func writeTcp(sess net.Conn, numBytes int) error {
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
