package tests

import (
	"log"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// go test -v -run=TestCachePubAddr
func TestCachePubAddr(t *testing.T) {
	ch := make(chan string, 1)

	go func() {
		StartTunaTcpListener(numTcpListener, ch)
	}()

	waitfor(ch, listening)
	time.Sleep(10 * time.Second) // still wait for tuna exits establish connections.

	go func() {
		log.Printf("tcp dialer start now...")

		acc, wal, err := CreateAccountAndWallet(seedHex)
		require.Nil(t, err)
		mc, err := CreateMultiClient(acc, dialerId, 2)
		require.Nil(t, err)

		tunaSess, err := CreateTunaSession(acc, wal, mc, numTcpListener)
		require.Nil(t, err)

		diaConfig := CreateDialConfig(5000)
		ncpSess1, err := tunaSess.DialWithConfig(remoteAddr, diaConfig)
		require.Nil(t, err)
		ch <- dialed

		mc.Close() // close nkn multi-client
		ncpSess2, err := tunaSess.DialWithConfig(remoteAddr, diaConfig)
		require.Nil(t, err)
		ch <- dialed
		log.Printf("tcp dialer dialed up even nkn multi-client closed, because it uses cached pubAddrs...")

		tunaSess.CloseOneConn(ncpSess1, "0") // close connection 0
		tunaSess.CloseOneConn(ncpSess2, "0") // close conenction 0

		time.Sleep(time.Second)
		_, err = tunaSess.DialWithConfig(remoteAddr, diaConfig) // should not dial up successfully.
		require.NotNil(t, err)

		ch <- end
	}()

	waitfor(ch, end)
}
