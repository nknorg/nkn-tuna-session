package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	ncp "github.com/nknorg/ncp-go"
	nkn "github.com/nknorg/nkn-sdk-go"
	"github.com/nknorg/tuna"
)

const (
	DefaultSessionAllowAddr = nkn.DefaultSessionAllowAddr
	SessionIDSize           = nkn.SessionIDSize
	acceptSessionBufSize    = 128
)

type TunaSessionClient struct {
	config        *Config
	clientAccount *nkn.Account
	multiClient   *nkn.MultiClient
	wallet        *nkn.Wallet
	addr          net.Addr
	acceptSession chan *ncp.Session
	onClose       chan struct{}

	sync.RWMutex
	listeners    []net.Listener
	tunaExits    []*tuna.TunaExit
	acceptAddrs  []*regexp.Regexp
	sessions     map[string]*ncp.Session
	sessionConns map[string]map[string]net.Conn
	sharedKeys   map[string]*[sharedKeySize]byte
	isClosed     bool
}

func NewTunaSessionClient(clientAccount *nkn.Account, m *nkn.MultiClient, wallet *nkn.Wallet, config *Config) (*TunaSessionClient, error) {
	config, err := MergedConfig(config)
	if err != nil {
		return nil, err
	}

	c := &TunaSessionClient{
		config:        config,
		clientAccount: clientAccount,
		multiClient:   m,
		wallet:        wallet,
		addr:          m.Addr(),
		acceptSession: make(chan *ncp.Session, acceptSessionBufSize),
		onClose:       make(chan struct{}, 0),
		sessions:      make(map[string]*ncp.Session),
		sessionConns:  make(map[string]map[string]net.Conn),
		sharedKeys:    make(map[string]*[sharedKeySize]byte),
	}

	return c, nil
}

func (c *TunaSessionClient) Address() string {
	return c.addr.String()
}

func (c *TunaSessionClient) Addr() net.Addr {
	return c.addr
}

func (c *TunaSessionClient) Listen(addrsRe *nkn.StringArray) error {
	var addrs []string
	if addrsRe == nil {
		addrs = []string{DefaultSessionAllowAddr}
	} else {
		addrs = addrsRe.Elems
	}

	var err error
	acceptAddrs := make([]*regexp.Regexp, len(addrs))
	for i := 0; i < len(acceptAddrs); i++ {
		acceptAddrs[i], err = regexp.Compile(addrs[i])
		if err != nil {
			return err
		}
	}

	c.Lock()
	defer c.Unlock()

	c.acceptAddrs = acceptAddrs

	if len(c.listeners) > 0 {
		return nil
	}

	if c.wallet == nil {
		return errors.New("wallet is empty")
	}

	dialTimeout := c.config.TunaDialTimeout / 1000
	if dialTimeout > math.MaxUint16 {
		dialTimeout = 0
	}
	tunaConfig := &tuna.ExitConfiguration{
		SubscriptionPrefix: c.config.TunaSubscriptionPrefix,
		Reverse:            true,
		ReverseRandomPorts: true,
		ReverseMaxPrice:    c.config.TunaMaxPrice,
		DialTimeout:        uint16(dialTimeout),
	}

	connected := make(chan struct{}, 1)
	listeners := make([]net.Listener, c.config.NumTunaListeners)
	exits := make([]*tuna.TunaExit, c.config.NumTunaListeners)

	for i := 0; i < len(listeners); i++ {
		listener, err := net.Listen("tcp", "127.0.0.1:")
		if err != nil {
			return err
		}

		listeners[i] = listener

		_, portStr, err := net.SplitHostPort(listener.Addr().String())
		if err != nil {
			return err
		}

		port, err := strconv.Atoi(portStr)
		if err != nil {
			return err
		}

		service := tuna.Service{
			Name: "session",
			TCP:  []int{port},
		}

		exits[i] = tuna.NewTunaExit(tunaConfig, []tuna.Service{service}, c.wallet)
		exits[i].StartReverse(service.Name)
		exits[i].OnEntryConnected(func() {
			select {
			case connected <- struct{}{}:
			default:
			}
		})
	}

	<-connected

	c.listeners = listeners
	c.tunaExits = exits

	go c.listenNKN()

	for i := 0; i < len(listeners); i++ {
		go c.listenNet(i)
	}

	return nil
}

func (c *TunaSessionClient) shouldAcceptAddr(addr string) bool {
	for _, allowAddr := range c.acceptAddrs {
		if allowAddr.MatchString(addr) {
			return true
		}
	}
	return false
}

func (c *TunaSessionClient) listenNKN() {
	for {
		msg := <-c.multiClient.OnMessage.C
		if !c.shouldAcceptAddr(msg.Src) {
			continue
		}
		req := &Request{}
		err := json.Unmarshal(msg.Data, req)
		if err != nil {
			log.Printf("Decode request error: %v", err)
			continue
		}
		switch strings.ToLower(req.Action) {
		case "getpubaddr":
			addrs := make([]PubAddr, 0, len(c.tunaExits))
			for i := 0; i < len(c.tunaExits); i++ {
				ip := c.tunaExits[i].GetReverseIP().String()
				ports := c.tunaExits[i].GetReverseTCPPorts()
				if len(ip) == 0 || len(ports) == 0 {
					continue
				}
				addr := PubAddr{
					IP:   ip,
					Port: ports[0],
				}
				addrs = append(addrs, addr)
			}
			if len(addrs) == 0 {
				log.Println("No entry available")
				continue
			}
			buf, err := json.Marshal(&PubAddrs{Addrs: addrs})
			if err != nil {
				log.Printf("Encode reply error: %v", err)
				continue
			}
			err = msg.Reply(buf)
			if err != nil {
				log.Printf("Send reply error: %v", err)
				continue
			}
		default:
			log.Printf("Unknown action %v", req.Action)
			continue
		}
	}
}

func (c *TunaSessionClient) listenNet(i int) {
	for {
		conn, err := c.listeners[i].Accept()
		if err != nil {
			log.Printf("Accept connection error: %v", err)
			time.Sleep(time.Second)
			continue
		}

		go func(conn net.Conn) {
			defer conn.Close()

			buf, err := readMessage(conn)
			if err != nil {
				log.Printf("Read message error: %v", err)
				return
			}

			remoteAddr := string(buf)

			buf, err = readMessage(conn)
			if err != nil {
				log.Printf("Read message error: %v", err)
				return
			}

			sessionID, err := c.decode(buf, remoteAddr)
			if err != nil {
				log.Printf("Decode message error: %v", err)
				return
			}

			sessionKey := sessionKey(remoteAddr, sessionID)

			c.Lock()
			sess, ok := c.sessions[sessionKey]
			if !ok {
				if !c.shouldAcceptAddr(remoteAddr) {
					return
				}
				connIDs := make([]string, c.config.NumTunaListeners)
				for j := 0; j < len(connIDs); j++ {
					connIDs[j] = connID(j)
				}
				sess, err = c.newSession(remoteAddr, sessionID, connIDs, c.config.SessionConfig)
				if err != nil {
					c.Unlock()
					return
				}
				c.sessions[sessionKey] = sess
				c.sessionConns[sessionKey] = make(map[string]net.Conn, c.config.NumTunaListeners)
			}
			c.sessionConns[sessionKey][connID(i)] = conn
			c.Unlock()

			if !ok {
				err := c.handleMsg(conn, sess, i)
				if err != nil {
					return
				}

				select {
				case c.acceptSession <- sess:
				default:
					log.Println("Accept session channel full, discard request...")
				}
			}

			c.handleConn(conn, sess, i)
		}(conn)
	}
}

func (c *TunaSessionClient) encode(message []byte, remoteAddr string) ([]byte, error) {
	remotePublicKey, err := nkn.ClientAddrToPubKey(remoteAddr)
	if err != nil {
		return nil, err
	}

	sharedKey, err := c.getOrComputeSharedKey(remotePublicKey)
	if err != nil {
		return nil, err
	}

	encrypted, nonce, err := encrypt(message, sharedKey)
	if err != nil {
		return nil, err
	}

	return append(nonce, encrypted...), nil
}

func (c *TunaSessionClient) decode(buf []byte, remoteAddr string) ([]byte, error) {
	if len(buf) <= nonceSize {
		return nil, errors.New("message too short")
	}

	remotePublicKey, err := nkn.ClientAddrToPubKey(remoteAddr)
	if err != nil {
		return nil, err
	}

	sharedKey, err := c.getOrComputeSharedKey(remotePublicKey)
	if err != nil {
		return nil, err
	}

	var nonce [nonceSize]byte
	copy(nonce[:], buf[:nonceSize])
	message, err := decrypt(buf[nonceSize:], nonce, sharedKey)
	if err != nil {
		return nil, err
	}

	return message, nil
}

func (c *TunaSessionClient) Dial(remoteAddr string) (net.Conn, error) {
	return c.DialSession(remoteAddr)
}

func (c *TunaSessionClient) DialSession(remoteAddr string) (*ncp.Session, error) {
	return c.DialWithConfig(remoteAddr, nil)
}

func (c *TunaSessionClient) DialWithConfig(remoteAddr string, config *DialConfig) (*ncp.Session, error) {
	config, err := MergeDialConfig(c.config.SessionConfig, config)
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	var cancel context.CancelFunc
	if config.DialTimeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, time.Duration(config.DialTimeout)*time.Millisecond)
		defer cancel()
	}

	buf, err := json.Marshal(&Request{Action: "getPubAddr"})
	if err != nil {
		return nil, err
	}

	respChan, err := c.multiClient.Send(nkn.NewStringArray(remoteAddr), buf, nil)
	if err != nil {
		return nil, err
	}

	var msg *nkn.Message
	select {
	case msg = <-respChan.C:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	pubAddrs := &PubAddrs{}
	err = json.Unmarshal(msg.Data, pubAddrs)
	if err != nil {
		return nil, err
	}

	sessionID, err := nkn.RandomBytes(SessionIDSize)
	if err != nil {
		return nil, err
	}

	var lock sync.Mutex
	var wg sync.WaitGroup
	conns := make(map[string]net.Conn, len(pubAddrs.Addrs))
	dialer := &net.Dialer{}
	for i := range pubAddrs.Addrs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			conn, err := dialer.DialContext(ctx, "tcp", fmt.Sprintf("%s:%d", pubAddrs.Addrs[i].IP, pubAddrs.Addrs[i].Port))
			if err != nil {
				log.Printf("Dial error: %v", err)
				return
			}

			err = writeMessage(conn, []byte(c.addr.String()))
			if err != nil {
				log.Printf("Write message error: %v", err)
				return
			}

			buf, err := c.encode(sessionID, remoteAddr)
			if err != nil {
				log.Printf("Encode message error: %v", err)
				return
			}

			err = writeMessage(conn, buf)
			if err != nil {
				log.Printf("Write message error: %v", err)
				return
			}

			lock.Lock()
			conns[connID(i)] = conn
			lock.Unlock()
		}(i)
	}
	wg.Wait()

	connIDs := make([]string, 0, len(conns))
	for i := 0; i < len(pubAddrs.Addrs); i++ {
		if _, ok := conns[connID(i)]; ok {
			connIDs = append(connIDs, connID(i))
		}
	}

	sessionKey := sessionKey(remoteAddr, sessionID)
	sess, err := c.newSession(remoteAddr, sessionID, connIDs, config.SessionConfig)
	if err != nil {
		return nil, err
	}

	c.Lock()
	c.sessions[sessionKey] = sess
	c.sessionConns[sessionKey] = conns
	c.Unlock()

	for i := 0; i < len(pubAddrs.Addrs); i++ {
		if conn, ok := conns[connID(i)]; ok {
			go func(conn net.Conn, i int) {
				defer conn.Close()
				c.handleConn(conn, sess, i)
			}(conn, i)
		}
	}

	err = sess.Dial(ctx)
	if err != nil {
		return nil, err
	}

	return sess, nil
}

func (c *TunaSessionClient) AcceptSession() (*ncp.Session, error) {
	for {
		select {
		case session := <-c.acceptSession:
			err := session.Accept()
			if err != nil {
				log.Println(err)
				continue
			}
			return session, nil
		case _, ok := <-c.onClose:
			if !ok {
				return nil, nkn.ErrClosed
			}
		}
	}
}

func (c *TunaSessionClient) Accept() (net.Conn, error) {
	return c.AcceptSession()
}

func (c *TunaSessionClient) Close() error {
	c.Lock()
	defer c.Unlock()

	if c.isClosed {
		return nil
	}

	err := c.multiClient.Close()
	if err != nil {
		log.Println(err)
	}

	for _, listener := range c.listeners {
		err := listener.Close()
		if err != nil {
			log.Println(err)
			continue
		}
	}

	for _, sess := range c.sessions {
		if !sess.IsClosed() {
			err := sess.Close()
			if err != nil {
				log.Println(err)
				continue
			}
		}
	}

	for _, conns := range c.sessionConns {
		for _, conn := range conns {
			err := conn.Close()
			if err != nil {
				log.Println(err)
				continue
			}
		}
	}

	for _, tunaExit := range c.tunaExits {
		tunaExit.Close()
	}

	c.isClosed = true

	close(c.onClose)

	return nil
}

func (c *TunaSessionClient) IsClosed() bool {
	c.RLock()
	defer c.RUnlock()
	return c.isClosed
}

func (c *TunaSessionClient) newSession(remoteAddr string, sessionID []byte, connIDs []string, config *ncp.Config) (*ncp.Session, error) {
	sessionKey := sessionKey(remoteAddr, sessionID)
	return ncp.NewSession(c.addr, nkn.NewClientAddr(remoteAddr), connIDs, nil, (func(connID, _ string, buf []byte, writeTimeout time.Duration) error {
		c.RLock()
		conn := c.sessionConns[sessionKey][connID]
		c.RUnlock()
		if conn == nil {
			return fmt.Errorf("conn %s is nil", connID)
		}
		if writeTimeout > 0 {
			err := conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			if err != nil {
				return err
			}
		}
		buf, err := c.encode(buf, remoteAddr)
		if err != nil {
			return err
		}
		err = writeMessage(conn, buf)
		if err != nil {
			return err
		}
		if writeTimeout > 0 {
			err = conn.SetWriteDeadline(zeroTime)
			if err != nil {
				return err
			}
		}
		return nil
	}), config)
}

func (c *TunaSessionClient) handleMsg(conn net.Conn, sess *ncp.Session, i int) error {
	buf, err := readMessage(conn)
	if err != nil {
		return err
	}

	buf, err = c.decode(buf, sess.RemoteAddr().String())
	if err != nil {
		return err
	}

	err = sess.ReceiveWith(connID(i), connID(i), buf)
	if err != nil {
		return err
	}

	return nil
}

func (c *TunaSessionClient) handleConn(conn net.Conn, sess *ncp.Session, i int) {
	for {
		err := c.handleMsg(conn, sess, i)
		if err != nil {
			if err == io.EOF || err == ncp.ErrSessionClosed || sess.IsClosed() {
				return
			}
			select {
			case _, ok := <-c.onClose:
				if !ok {
					return
				}
			default:
			}
			log.Printf("handle msg error: %v", err)
		}
	}
}
