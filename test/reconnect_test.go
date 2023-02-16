package test

import (
	"testing"
	"time"

	"github.com/nknorg/ncp-go"
	ts "github.com/nknorg/nkn-tuna-session"
)

var bytesToSend int = 30 << 20
var numListener int = 4

// go test -v -run=TestNormalListener
func TestNormalListener(t *testing.T) {
	ch := make(chan string, 1)

	go func() {
		StartTunaListner(numListener, ch)
	}()

	<-ch // started
	<-ch // end
}

// go test -v -run=TestNormalDialer
func TestNormalDialer(t *testing.T) {
	ch := make(chan string, 1)

	go func() {
		// wait for Listener be ready
		time.Sleep(2 * time.Second)
		StartTunaDialer(bytesToSend, numListener, ch)
	}()

	<-ch
	<-ch
}

// go test -v -run=TestCloseOneConnListener
func TestCloseOneConnListener(t *testing.T) {
	var tunaSess *ts.TunaSessionClient
	var ncpSess *ncp.Session
	ch := make(chan string, 1)

	go func() {
		tunaSess, ncpSess = StartTunaListner(numListener, ch)
	}()

	<-ch // started
	time.Sleep(3 * time.Second)
	tunaSess.CloseOneConn(ncpSess, "2")

	<-ch // end
}

// go test -v -run=TestCloseOneConnDialer
func TestCloseOneConnDialer(t *testing.T) {
	var tunaSess *ts.TunaSessionClient
	var ncpSess *ncp.Session
	ch := make(chan string, 1)

	go func() {
		// wait for Listener be ready
		time.Sleep(2 * time.Second)
		tunaSess, ncpSess = StartTunaDialer(bytesToSend, numListener, ch)
	}()

	<-ch
	time.Sleep(2 * time.Second)
	tunaSess.CloseOneConn(ncpSess, "1")

	<-ch
}

// go test -v -run=TestCloseAllConnDialer
func TestCloseAllConnDialer(t *testing.T) {
	var tunaSess *ts.TunaSessionClient
	var ncpSess *ncp.Session
	ch := make(chan string, 1)

	go func() {
		// wait for Listener be ready
		time.Sleep(2 * time.Second)
		tunaSess, ncpSess = StartTunaDialer(bytesToSend, numListener, ch)
	}()

	<-ch
	time.Sleep(2 * time.Second)
	tunaSess.CloseOneConn(ncpSess, "0")
	time.Sleep(2 * time.Second)
	tunaSess.CloseOneConn(ncpSess, "1")
	time.Sleep(2 * time.Second)
	tunaSess.CloseOneConn(ncpSess, "2")
	time.Sleep(2 * time.Second)
	tunaSess.CloseOneConn(ncpSess, "3")

	<-ch
}
