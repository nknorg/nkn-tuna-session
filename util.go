package session

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"
)

var (
	zeroTime time.Time
)

func sessionKey(remoteAddr string, sessionID []byte) string {
	return remoteAddr + ":" + hex.EncodeToString(sessionID)
}

func connID(i int) string {
	return strconv.Itoa(i)
}

// Get free port start from parameter `port`
// If paramenter `port` is 0, return system available port
// The returned port is free in both TCP and UDP
var lock sync.Mutex

func GetFreePort(port int) (int, error) {
	// to avoid race condition
	lock.Lock()
	defer lock.Unlock()

	for i := 0; i < 100; i++ {
		addr, err := net.ResolveTCPAddr("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			return 0, err
		}

		l, err := net.ListenTCP("tcp", addr)
		if err != nil {
			return 0, err
		}
		defer l.Close()

		port = l.Addr().(*net.TCPAddr).Port
		u, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: port})
		if err != nil {
			l.Close()
			port++
			continue
		}
		u.Close()

		return port, nil
	}

	return 0, errors.New("failed to find free port after 100 tries")
}
