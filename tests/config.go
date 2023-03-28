package tests

import "time"

const (
	bytesToSend    = 10 << 10
	numTcpListener = 2
	numUdpListener = 1
	numUdpDialers  = 4
	bufSize        = 100
	writeInterval  = 2 * time.Millisecond

	listenerId = "Bob"
	dialerId   = "Alice"
	seedHex    = "e68e046d13dd911594576ba0f4a196e9666790dc492071ad9ea5972c0b940435"
	remoteAddr = "Bob.be285ff9330122cea44487a9618f96603fde6d37d5909ae1c271616772c349fe"
)
