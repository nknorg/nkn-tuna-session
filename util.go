package session

import (
	"encoding/hex"
	"strconv"
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
