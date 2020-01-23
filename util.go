package session

import (
	"strconv"
	"time"
)

var (
	zeroTime time.Time
)

func sessionKey(remoteAddr string, sessionID []byte) string {
	return remoteAddr + string(sessionID)
}

func connID(i int) string {
	return strconv.Itoa(i)
}
