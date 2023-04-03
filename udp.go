package session

import (
	"errors"
	"net"
	"strconv"
	"time"

	"github.com/nknorg/nkn-sdk-go"
	"github.com/nknorg/nkngomobile"
	"github.com/nknorg/tuna"
)

const (
	sendUdpMetaDataRetries = 2 // retries to send udp meta data
)

func (c *TunaSessionClient) ListenUDP(addrsRe *nkngomobile.StringArray) (*UdpSession, error) {
	acceptAddrs, err := getAcceptAddrs(addrsRe)
	if err != nil {
		return nil, err
	}
	if c.wallet == nil {
		return nil, errors.New("ListenUDP wallet is empty")
	}

	c.acceptAddrs = acceptAddrs

	if c.listenerUdpSess != nil {
		return c.listenerUdpSess, nil
	}
	c.listenerUdpSess = newUdpSession(c, true)

	err = c.startExits()
	if err != nil {
		return nil, err
	}

	host, portStr, err := net.SplitHostPort(c.listeners[0].Addr().String())
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, err
	}

	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP(host), Port: port})
	if err != nil {
		return nil, err
	}

	c.listenerUdpSess.udpConn = tuna.NewEncryptUDPConn(conn)

	go c.listenerUdpSess.recvMsg()

	return c.listenerUdpSess, nil
}

func (c *TunaSessionClient) DialUDP(remoteAddr string) (*UdpSession, error) {
	return c.DialUDPWithConfig(remoteAddr, nil)
}

func (c *TunaSessionClient) DialUDPWithConfig(remoteAddr string, config *nkn.DialConfig) (*UdpSession, error) {
	config, err := nkn.MergeDialConfig(c.config.SessionConfig, config)
	if err != nil {
		return nil, err
	}

	remoteAddr, err = c.multiClient.ResolveDest(remoteAddr)
	if err != nil {
		return nil, err
	}

	sessionID, err := nkn.RandomBytes(SessionIDSize)
	if err != nil {
		return nil, err
	}

	udpSess := newUdpSession(c, false)
	err = udpSess.DialUpSession(remoteAddr, sessionID, config)
	if err != nil {
		return nil, err
	}
	go udpSess.recvMsg()

	go func() {
		for {
			udpSess.handleTcpMsg(udpSess.tcpConn, sessionKey(remoteAddr, sessionID))

			if c.IsClosed() || udpSess.udpConn.IsClosed() {
				break
			}
			if c.config.ReconnectRetries == 0 {
				return
			}

			j := 0
			for j = 0; c.config.ReconnectRetries < 0 || j < c.config.ReconnectRetries; j++ {
				var err error
				if err = udpSess.DialUpSession(remoteAddr, sessionID, config); err == nil {
					break
				}
				if err == ErrClosed {
					return
				}
				time.Sleep(time.Duration(c.config.ReconnectInterval) * time.Millisecond)
			}
			if j >= c.config.ReconnectRetries {
				break
			}
		}
	}()

	return udpSess, nil
}

func (c *TunaSessionClient) handleUdpListenerTcp(tcpConn *Conn, remoteAddr string, sessionID []byte) {
	sessKey := sessionKey(remoteAddr, sessionID)

	c.listenerUdpSess.Lock()
	c.listenerUdpSess.tcpConns[sessKey] = tcpConn
	c.listenerUdpSess.Unlock()

	go func() { // Udp listener starts monitoring tcp connection status
		c.listenerUdpSess.handleTcpMsg(tcpConn, sessKey)

		c.listenerUdpSess.Lock()
		delete(c.listenerUdpSess.tcpConns, sessKey)
		c.listenerUdpSess.Unlock()
	}()
}
