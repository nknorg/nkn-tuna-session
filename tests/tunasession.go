package tests

import (
	"fmt"
	"log"
	"time"

	"github.com/nknorg/ncp-go"
	ts "github.com/nknorg/nkn-tuna-session"
)

const seedHex = "e68e046d13dd911594576ba0f4a196e9666790dc492071ad9ea5972c0b940435"
const listenerId = "Bob"
const dialerId = "Alice"

const remoteAddr = "Bob.be285ff9330122cea44487a9618f96603fde6d37d5909ae1c271616772c349fe"

func StartTunaListner(numListener int, ch chan string) (tunaSess *ts.TunaSessionClient, ncpSess *ncp.Session) {
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

	sess, err := tunaSess.Accept()
	if err != nil {
		log.Fatal("tunaSess.Accept ", err)
	}
	ncpSess = sess.(*ncp.Session)
	ch <- "started"

	go func() {
		err = read(ncpSess)
		if err != nil {
			fmt.Printf("StartTunaListner read err:%v\n", err)
		} else {
			fmt.Printf("Finished reading, close ncp.session now\n")
		}
		ncpSess.Close()
		ch <- "end"
	}()

	return
}

func StartTunaDialer(numBytes int, numListener int, ch chan string) (tunaSess *ts.TunaSessionClient, ncpSess *ncp.Session) {
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
	ch <- "started"

	go func() {
		err = write(ncpSess, numBytes)
		if err != nil {
			fmt.Printf("StartTunaDialer write err:%v\n", err)
		} else {
			fmt.Printf("Finished writing, close ncp.session now\n")
		}
		time.Sleep(time.Second) // wait for reader to read data.
		ncpSess.Close()
		ch <- "end"
	}()

	return
}
