package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/imdario/mergo"
	"github.com/nknorg/ncp-go"
	"github.com/nknorg/nkn-sdk-go"
	"github.com/nknorg/nkn-tuna-session/pb"
	"github.com/nknorg/nkngomobile"
	"github.com/nknorg/tuna"
	tpb "github.com/nknorg/tuna/pb"
	"github.com/nknorg/tuna/udp"
	gocache "github.com/patrickmn/go-cache"
	"google.golang.org/protobuf/proto"
)

const (
	DefaultSessionAllowAddr         = nkn.DefaultSessionAllowAddr
	SessionIDSize                   = nkn.SessionIDSize
	acceptSessionBufSize            = 1024
	closedSessionKeyExpiration      = 5 * time.Minute
	closedSessionKeyCleanupInterval = time.Minute
)

var (
	ErrClosed = errors.New("tuna session is closed")
)

type TunaSessionClient struct {
	config        *Config
	clientAccount *nkn.Account
	multiClient   *nkn.MultiClient
	wallet        *nkn.Wallet
	addr          net.Addr
	acceptSession chan *ncp.Session
	onConnect     chan struct{}
	onClose       chan struct{}
	connectedOnce sync.Once
	udpConn       *udp.EncryptUDPConn

	sync.RWMutex
	listeners        []net.Listener
	tunaExits        []*tuna.TunaExit
	acceptAddrs      []*regexp.Regexp
	sessions         map[string]*ncp.Session
	sessionConns     map[string]map[string]*Conn
	sharedKeys       map[string]*[sharedKeySize]byte
	connCount        map[string]int
	closedSessionKey *gocache.Cache
	isClosed         bool
}

func NewTunaSessionClient(clientAccount *nkn.Account, m *nkn.MultiClient, wallet *nkn.Wallet, config *Config) (*TunaSessionClient, error) {
	config, err := MergedConfig(config)
	if err != nil {
		return nil, err
	}

	c := &TunaSessionClient{
		config:           config,
		clientAccount:    clientAccount,
		multiClient:      m,
		wallet:           wallet,
		addr:             m.Addr(),
		acceptSession:    make(chan *ncp.Session, acceptSessionBufSize),
		onConnect:        make(chan struct{}),
		onClose:          make(chan struct{}),
		sessions:         make(map[string]*ncp.Session),
		sessionConns:     make(map[string]map[string]*Conn),
		sharedKeys:       make(map[string]*[sharedKeySize]byte),
		connCount:        make(map[string]int),
		closedSessionKey: gocache.New(closedSessionKeyExpiration, closedSessionKeyCleanupInterval),
	}

	go c.removeClosedSessions()

	return c, nil
}

func (c *TunaSessionClient) Address() string {
	return c.addr.String()
}

func (c *TunaSessionClient) Addr() net.Addr {
	return c.addr
}

// SetConfig will set any non-empty value in conf to tuna session config.
func (c *TunaSessionClient) SetConfig(conf *Config) error {
	c.Lock()
	defer c.Unlock()
	err := mergo.Merge(c.config, conf, mergo.WithOverride)
	if err != nil {
		return err
	}
	if conf.TunaIPFilter != nil {
		c.config.TunaIPFilter = conf.TunaIPFilter
	}
	if conf.TunaNknFilter != nil {
		c.config.TunaNknFilter = conf.TunaNknFilter
	}
	return nil
}

func (c *TunaSessionClient) newTunaExit(i int) (*tuna.TunaExit, error) {
	if i >= len(c.listeners) {
		return nil, errors.New("index out of range")
	}

	_, portStr, err := net.SplitHostPort(c.listeners[i].Addr().String())
	if err != nil {
		return nil, err
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, err
	}

	service := tuna.Service{
		Name: "session",
		TCP:  []uint32{uint32(port)},
		UDP:  []uint32{uint32(port)},
	}

	tunaConfig := &tuna.ExitConfiguration{
		Reverse:                   true,
		ReverseRandomPorts:        true,
		ReverseMaxPrice:           c.config.TunaMaxPrice,
		ReverseNanoPayFee:         c.config.TunaNanoPayFee,
		MinReverseNanoPayFee:      c.config.TunaMinNanoPayFee,
		ReverseNanoPayFeeRatio:    c.config.TunaNanoPayFeeRatio,
		ReverseServiceName:        c.config.TunaServiceName,
		ReverseSubscriptionPrefix: c.config.TunaSubscriptionPrefix,
		ReverseIPFilter:           *c.config.TunaIPFilter,
		ReverseNknFilter:          *c.config.TunaNknFilter,
		DownloadGeoDB:             c.config.TunaDownloadGeoDB,
		GeoDBPath:                 c.config.TunaGeoDBPath,
		MeasureBandwidth:          c.config.TunaMeasureBandwidth,
		MeasureStoragePath:        c.config.TunaMeasureStoragePath,
		DialTimeout:               int32(c.config.TunaDialTimeout / 1000),
		SortMeasuredNodes:         sortMeasuredNodes,
	}

	return tuna.NewTunaExit([]tuna.Service{service}, c.wallet, nil, tunaConfig)
}

func (c *TunaSessionClient) Listen(addrsRe *nkngomobile.StringArray) error {
	acceptAddrs, err := getAcceptAddrs(addrsRe)
	if err != nil {
		return err
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

	return c.startExits()
}

// OnConnect returns a channel that will be closed when at least one tuna exit
// is connected after Listen() is first called.
func (c *TunaSessionClient) OnConnect() chan struct{} {
	return c.onConnect
}

// RotateOne create a new tuna exit and replace the i-th one. New connections
// accepted will use new tuna exit, existing connections will not be affected.
func (c *TunaSessionClient) RotateOne(i int) error {
	c.RLock()
	te, err := c.newTunaExit(i)
	if err != nil {
		c.RUnlock()
		return err
	}
	c.RUnlock()

	go te.StartReverse(true)

	<-te.OnConnect.C

	c.Lock()
	oldTe := c.tunaExits[i]
	c.tunaExits[i] = te
	c.Unlock()

	if oldTe != nil {
		oldTe.SetLinger(-1)
		go oldTe.Close()
	}

	return nil
}

// RotateAll create and replace all tuna exit. New connections accepted will use
// new tuna exit, existing connections will not be affected.
func (c *TunaSessionClient) RotateAll() error {
	c.RLock()
	n := len(c.listeners)
	c.RUnlock()

	for i := 0; i < n; i++ {
		err := c.RotateOne(i)
		if err != nil {
			return err
		}
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

func (c *TunaSessionClient) getPubAddrsFromRemote(ctx context.Context, remoteAddr string) (*PubAddrs, error) {
	buf, err := json.Marshal(&Request{Action: "getPubAddr"})
	if err != nil {
		return nil, err
	}

	msgChan := make(chan *nkn.Message, 1)
	errChan := make(chan error, 1)
	doneCtx, doneCancel := context.WithCancel(ctx)
	defer doneCancel()
	wg := sync.WaitGroup{}
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			select {
			case <-time.After(time.Duration(3*i) * time.Second):
			case <-doneCtx.Done():
				return
			}
			respChan, err := c.multiClient.Send(nkn.NewStringArray(remoteAddr), buf, nil)
			if err != nil {
				select {
				case errChan <- err:
				default:
				}
				return
			}
			select {
			case msg := <-respChan.C:
				select {
				case msgChan <- msg:
				default:
				}
				doneCancel()
			case <-doneCtx.Done():
			}
		}(i)
	}
	wg.Wait()
	close(msgChan)
	close(errChan)

	msg, ok := <-msgChan
	if !ok {
		err, ok := <-errChan
		if ok {
			return nil, err
		}
		return nil, ctx.Err()
	}

	pubAddrs := &PubAddrs{}
	err = json.Unmarshal(msg.Data, pubAddrs)
	if err != nil {
		return nil, err
	}
	for _, addr := range pubAddrs.Addrs {
		if len(addr.IP) > 0 && addr.Port != 0 {
			return pubAddrs, nil
		}
	}
	return nil, errors.New("no pubAddrs available")
}

func (c *TunaSessionClient) getPubAddrs(includePrice bool) *PubAddrs {
	if c.tunaExits == nil {
		return nil
	}
	addrs := make([]*PubAddr, 0, len(c.tunaExits))
	for _, tunaExit := range c.tunaExits {
		ip := tunaExit.GetReverseIP().String()
		ports := tunaExit.GetReverseTCPPorts()
		addr := &PubAddr{}
		if len(ip) > 0 && len(ports) > 0 {
			addr.IP = ip
			addr.Port = ports[0]
			if includePrice {
				entryToExitPrice, exitToEntryPrice := tunaExit.GetPrice()
				addr.InPrice = entryToExitPrice.String()
				addr.OutPrice = exitToEntryPrice.String()
			}
		}
		addrs = append(addrs, addr)
	}
	return &PubAddrs{Addrs: addrs}
}

func (c *TunaSessionClient) GetPubAddrs() *PubAddrs {
	return c.getPubAddrs(true)
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
			pubAddrs := c.getPubAddrs(false)
			if len(pubAddrs.Addrs) == 0 {
				log.Println("No entry available")
				continue
			}
			buf, err := json.Marshal(pubAddrs)
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
		netConn, err := c.listeners[i].Accept()
		if err != nil {
			log.Printf("Accept connection error: %v", err)
			time.Sleep(time.Second)
			continue
		}

		conn := newConn(netConn)

		go func(conn *Conn) {
			defer conn.Close()

			buf, err := readMessage(conn, maxAddrSize)
			if err != nil {
				log.Printf("Read message error: %v", err)
				return
			}

			remoteAddr := string(buf)

			if !c.shouldAcceptAddr(remoteAddr) {
				return
			}

			buf, err = readMessage(conn, maxSessionMetadataSize)
			if err != nil {
				log.Printf("Read message error: %v", err)
				return
			}

			metadataRaw, err := c.decode(buf, remoteAddr)
			if err != nil {
				log.Printf("Decode message error: %v", err)
				return
			}

			metadata := &pb.SessionMetadata{}
			err = proto.Unmarshal(metadataRaw, metadata)
			if err != nil {
				log.Printf("Decode session metadata error: %v", err)
				return
			}

			sessionID := metadata.Id
			sessKey := sessionKey(remoteAddr, sessionID)

			c.Lock()
			sess, ok := c.sessions[sessKey]
			if !ok {
				if _, ok := c.closedSessionKey.Get(sessKey); ok {
					c.Unlock()
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
				c.sessions[sessKey] = sess
				c.sessionConns[sessKey] = make(map[string]*Conn, c.config.NumTunaListeners)
			}
			c.sessionConns[sessKey][connID(i)] = conn
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

			c.handleConn(conn, sessKey, i)
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

func (c *TunaSessionClient) DialWithConfig(remoteAddr string, config *nkn.DialConfig) (*ncp.Session, error) {
	config, err := nkn.MergeDialConfig(c.config.SessionConfig, config)
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	var cancel context.CancelFunc
	if config.DialTimeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, time.Duration(config.DialTimeout)*time.Millisecond)
		defer cancel()
	}

	pubAddrs, err := c.getPubAddrsFromRemote(ctx, remoteAddr)
	if err != nil {
		return nil, err
	}

	sessionID, err := nkn.RandomBytes(SessionIDSize)
	if err != nil {
		return nil, err
	}

	var wg sync.WaitGroup
	for i := range pubAddrs.Addrs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			_, err = c.dialAConn(ctx, remoteAddr, sessionID, i, pubAddrs.Addrs[i], config)
			if err != nil {
				log.Printf("Tuna session dial to ip %v port %v err: %v", pubAddrs.Addrs[i].IP, pubAddrs.Addrs[i].Port, err)
				return
			}
		}(i)
	}

	wg.Wait()
	sessKey := sessionKey(remoteAddr, sessionID)

	c.RLock()
	conns := c.sessionConns[sessKey]
	if len(conns) == 0 {
		c.RUnlock()
		return nil, err
	}
	connIDs := make([]string, 0, len(conns))
	for i := 0; i < len(pubAddrs.Addrs); i++ {
		if _, ok := conns[connID(i)]; ok {
			connIDs = append(connIDs, connID(i))
		}
	}
	c.RUnlock()

	sess, err := c.newSession(remoteAddr, sessionID, connIDs, config.SessionConfig)
	if err != nil {
		return nil, err
	}
	c.Lock()
	c.sessions[sessKey] = sess
	c.Unlock()

	for i := 0; i < len(pubAddrs.Addrs); i++ {
		c.RLock()
		conn, ok := conns[connID(i)]
		c.RUnlock()
		if ok {
			go func(conn *Conn, i int) {
				defer conn.Close()
				for {
					c.handleConn(conn, sessKey, i)

					if c.config.ReconnectRetries == 0 {
						return
					}
					for j := 0; c.config.ReconnectRetries < 0 || j < c.config.ReconnectRetries; j++ {
						var err error
						if conn, err = c.reconnect(remoteAddr, sessionID, i, pubAddrs, config); err == nil {
							break
						}
						if err == ErrClosed || err == ncp.ErrSessionClosed {
							return
						}
						time.Sleep(time.Duration(c.config.ReconnectInterval) * time.Millisecond)
					}
				}
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
				log.Println("Accept error:", err)
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
		log.Println("MultiClient close error:", err)
	}

	for _, listener := range c.listeners {
		err := listener.Close()
		if err != nil {
			log.Println("Listener close error:", err)
			continue
		}
	}

	for _, sess := range c.sessions {
		if !sess.IsClosed() {
			err := sess.Close()
			if err != nil {
				log.Println("Session close error:", err)
				continue
			}
		}
	}

	for _, conns := range c.sessionConns {
		for _, conn := range conns {
			err := conn.Close()
			if err != nil {
				log.Println("Conn close error:", err)
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
	sessKey := sessionKey(remoteAddr, sessionID)
	return ncp.NewSession(c.addr, nkn.NewClientAddr(remoteAddr), connIDs, nil, func(connID, _ string, buf []byte, writeTimeout time.Duration) error {
		c.RLock()
		conn := c.sessionConns[sessKey][connID]
		c.RUnlock()
		if conn == nil {
			return fmt.Errorf("conn %s is nil", connID)
		}
		buf, err := c.encode(buf, remoteAddr)
		if err != nil {
			return err
		}
		err = writeMessage(conn, buf, writeTimeout)
		if err != nil {
			log.Println("Write message error:", err)
			conn.Close()
			return err // ncp.ErrConnClosed
		}
		return nil
	}, config)
}

func (c *TunaSessionClient) handleMsg(conn *Conn, sess *ncp.Session, i int) error {
	buf, err := readMessage(conn, uint32(c.config.SessionConfig.MTU+maxSessionMsgOverhead))
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

func (c *TunaSessionClient) handleConn(conn *Conn, sessKey string, i int) {
	if conn == nil {
		return
	}
	c.Lock()
	sess := c.sessions[sessKey]
	if sess == nil {
		c.Unlock()
		return
	}
	c.connCount[sessKey]++
	c.Unlock()

	defer func() {
		c.Lock()
		c.connCount[sessKey]--
		shouldClose := c.connCount[sessKey] == 0
		if shouldClose {
			delete(c.sessions, sessKey)
			delete(c.sessionConns, sessKey)
			delete(c.connCount, sessKey)
			c.closedSessionKey.Add(sessKey, nil, gocache.DefaultExpiration)
		}
		c.Unlock()

		if shouldClose {
			sess.Close()
		}
	}()

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
			return
		}
	}
}

func (c *TunaSessionClient) removeClosedSessions() {
	for {
		time.Sleep(time.Second)

		if c.IsClosed() {
			return
		}

		c.Lock()
		for sessKey, sess := range c.sessions {
			if sess.IsClosed() {
				for _, conn := range c.sessionConns[sessKey] {
					conn.Close()
				}
				delete(c.sessions, sessKey)
				delete(c.sessionConns, sessKey)
				delete(c.connCount, sessKey)
				c.closedSessionKey.Add(sessKey, nil, gocache.DefaultExpiration)
			}
		}
		c.Unlock()
	}
}

func (c *TunaSessionClient) startExits() error {
	if len(c.tunaExits) > 0 {
		return nil
	}
	listeners := make([]net.Listener, c.config.NumTunaListeners)
	var err error
	for i := 0; i < len(listeners); i++ {
		listeners[i], err = net.Listen("tcp", "127.0.0.1:")
		if err != nil {
			return err
		}
	}
	c.listeners = listeners

	exits := make([]*tuna.TunaExit, c.config.NumTunaListeners)
	for i := 0; i < len(listeners); i++ {
		exits[i], err = c.newTunaExit(i)
		if err != nil {
			return err
		}

		go func(te *tuna.TunaExit) {
			select {
			case <-te.OnConnect.C:
				c.connectedOnce.Do(func() {
					close(c.onConnect)
				})
			case <-c.onClose:
				return
			}
		}(exits[i])

		go exits[i].StartReverse(true)
	}
	c.tunaExits = exits
	go func() {
		select {
		case <-c.onConnect:
			go c.listenNKN()
			for i := 0; i < len(listeners); i++ {
				go c.listenNet(i)
			}
		case <-c.onClose:
			return
		}
	}()

	return nil
}

func (c *TunaSessionClient) ListenUDP(addrsRe *nkngomobile.StringArray) (*udp.EncryptUDPConn, error) {
	acceptAddrs, err := getAcceptAddrs(addrsRe)
	if err != nil {
		return nil, err
	}

	c.Lock()
	defer c.Unlock()

	c.acceptAddrs = acceptAddrs

	if c.udpConn != nil {
		return c.udpConn, nil
	}

	if c.wallet == nil {
		return nil, errors.New("wallet is empty")
	}

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

	c.udpConn = udp.NewEncryptUDPConn(conn)
	return c.udpConn, nil
}

func (c *TunaSessionClient) DialUDP(remoteAddr string) (*udp.EncryptUDPConn, error) {
	return c.DialUDPWithConfig(remoteAddr, nil)
}

func (c *TunaSessionClient) DialUDPWithConfig(remoteAddr string, config *nkn.DialConfig) (*udp.EncryptUDPConn, error) {
	config, err := nkn.MergeDialConfig(c.config.SessionConfig, config)
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	var cancel context.CancelFunc
	if config.DialTimeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, time.Duration(config.DialTimeout)*time.Millisecond)
		defer cancel()
	}
	pubAddrs, err := c.getPubAddrsFromRemote(ctx, remoteAddr)
	if err != nil {
		return nil, err
	}
	remotePublicKey, err := nkn.ClientAddrToPubKey(remoteAddr)
	if err != nil {
		return nil, err
	}
	_, err = c.getOrComputeSharedKey(remotePublicKey)
	if err != nil {
		return nil, err
	}

	udpConn := new(udp.EncryptUDPConn)
	for i, addr := range pubAddrs.Addrs {
		if len(addr.IP) > 0 && addr.Port > 0 {
			udpAddr := net.UDPAddr{IP: net.ParseIP(addr.IP), Port: int(addr.Port)}
			conn, err := net.DialUDP("udp", nil, &udpAddr)
			if err != nil {
				if i == len(pubAddrs.Addrs)-1 {
					return nil, err
				}
				log.Printf("dial udp err: %v", err)
				continue
			}
			udpConn = udp.NewEncryptUDPConn(conn)
			break
		}
	}
	host, portStr, err := net.SplitHostPort(udpConn.LocalAddr().String())
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, err
	}
	err = udpConn.AddCodec(&net.UDPAddr{IP: net.ParseIP(host), Port: port}, c.sharedKeys[remoteAddr], tpb.EncryptionAlgo_ENCRYPTION_XSALSA20_POLY1305, false)
	if err != nil {
		return nil, err
	}

	return udpConn, nil
}

func (c *TunaSessionClient) ServicePort() (int, error) {
	for _, te := range c.tunaExits {
		ip := te.GetReverseIP().String()
		ports := te.GetReverseTCPPorts()
		if len(ip) > 0 && len(ports) > 0 {
			if len(te.ReverseMetadata.ServiceUdp) > 0 {
				return int(te.ReverseMetadata.ServiceUdp[0]), nil
			}
		}
	}
	return 0, errors.New("no tuna exit available")
}

func (c *TunaSessionClient) dialAConn(ctx context.Context, remoteAddr string, sessionID []byte, i int, addr *PubAddr, config *nkn.DialConfig) (conn *Conn, err error) {
	if addr == nil || len(addr.IP) == 0 || addr.Port == 0 {
		return nil, fmt.Errorf("dialAConn wrong params addr")
	}

	dialer := &net.Dialer{}
	netConn, err := dialer.DialContext(ctx, "tcp", fmt.Sprintf("%s:%d", addr.IP, addr.Port))
	if err != nil {
		return nil, err
	}
	conn = newConn(netConn)

	err = writeMessage(conn, []byte(c.addr.String()), time.Duration(config.DialTimeout)*time.Millisecond)
	if err != nil {
		conn.Close()
		return nil, err
	}

	metadata := &pb.SessionMetadata{
		Id: sessionID,
	}
	metadataRaw, err := proto.Marshal(metadata)
	if err != nil {
		conn.Close()
		return nil, err
	}

	buf, err := c.encode(metadataRaw, remoteAddr)
	if err != nil {
		conn.Close()
		return nil, err
	}

	err = writeMessage(conn, buf, time.Duration(config.DialTimeout)*time.Millisecond)
	if err != nil {
		conn.Close()
		return nil, err
	}

	sessKey := sessionKey(remoteAddr, sessionID)
	c.Lock()
	conns, ok := c.sessionConns[sessKey]
	if !ok {
		conns = make(map[string]*Conn)
	}
	conns[connID(i)] = conn
	c.sessionConns[sessKey] = conns
	c.Unlock()

	return conn, nil
}

func (c *TunaSessionClient) reconnect(remoteAddr string, sessionID []byte, i int, pubAddrs *PubAddrs, config *nkn.DialConfig) (conn *Conn, err error) {
	if c.IsClosed() {
		return nil, ErrClosed
	}
	sessKey := sessionKey(remoteAddr, sessionID)
	c.RLock()
	sess := c.sessions[sessKey]
	c.RUnlock()
	if sess == nil || sess.IsClosed() {
		return nil, ncp.ErrSessionClosed
	}

	ctx := context.Background()
	var cancel context.CancelFunc
	if config.DialTimeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, time.Duration(config.DialTimeout)*time.Millisecond)
		defer cancel()
	}

	newPubAddrs, err := c.getPubAddrsFromRemote(ctx, remoteAddr)
	if err != nil {
		return nil, err
	}
	if len(newPubAddrs.Addrs) > i {
		conn, err = c.dialAConn(ctx, remoteAddr, sessionID, i, newPubAddrs.Addrs[i], config)
		if err == nil {
			return conn, nil
		}
	}
	return nil, err
}

func (c *TunaSessionClient) CloseOneConn(sess *ncp.Session, connId string) {
	c.RLock()
	defer c.RUnlock()
	for key, s := range c.sessions {
		if s == sess {
			conn := c.sessionConns[key][connId]
			conn.Close()
			break
		}
	}
}
