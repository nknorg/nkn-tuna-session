package session

import (
	"net"
	"sync"
)

type Conn struct {
	net.Conn
	ReadLock  sync.Mutex
	WriteLock sync.Mutex
}

func newConn(conn net.Conn) *Conn {
	return &Conn{Conn: conn}
}
