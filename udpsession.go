package session

import (
	"context"
	"log"
	"net"
	"sync"
	"time"

	"github.com/nknorg/nkn-sdk-go"
	"github.com/nknorg/nkn-tuna-session/pb"
	"github.com/nknorg/tuna"
	tpb "github.com/nknorg/tuna/pb"
	"google.golang.org/protobuf/proto"
)

// Wrap of UDPAddr, including session information
// implement of net.Addr interface
type udpSessionAddr struct {
	sessionID     []byte
	remoteNknAddr string
	udpAddr       *net.UDPAddr // peer udp address.
}

func newUdpSessionAddr(sessionID []byte, remoteNknAddr string, udpAddr *net.UDPAddr) *udpSessionAddr {
	return &udpSessionAddr{sessionID: sessionID, remoteNknAddr: remoteNknAddr, udpAddr: udpAddr}
}
func (usa *udpSessionAddr) Network() string {
	return "tsudp"
}

// Return session key
func (usa *udpSessionAddr) String() string {
	return sessionKey(usa.remoteNknAddr, usa.sessionID)
}

type recvData struct {
	data []byte
	usa  *udpSessionAddr
}

// UDP session, it is a UDP listener or dialer endpoint for reading and writing udp data.
// After tuna session set up UDP connection, it return UpdSession to applications.
// Applications use this end point to read and write data.
// If there is a node break down between UDP listener and dialer, udp session will reconenct automatically
// without interrupting application usage.
type UdpSession struct {
	isListener  bool // true for listener, false for dialer
	udpConn     *tuna.EncryptUDPConn
	ts          *TunaSessionClient // Reference of tuna session
	recvChan    chan *recvData     // recv buffer for user application recvFrom
	recvSize    int                // size of buffer received msg used
	readContext context.Context
	readCancel  context.CancelFunc

	sync.RWMutex

	// dialer only
	tcpConn *Conn           // TCP *Conn, dialer only
	peer    *udpSessionAddr // dialer only

	// listener only
	sessKeyToSessAddr map[string]*udpSessionAddr // listener only, session key to to *udpSessionAddr
	udpAddrToSessAddr map[string]*udpSessionAddr // listener only, UDPAddr.String() to *udpSessionAddr
	tcpConns          map[string]*Conn           // listen only, session key to *Conn
}

func newUdpSession(ts *TunaSessionClient, isListener bool) *UdpSession {
	us := &UdpSession{ts: ts, isListener: isListener, recvChan: make(chan *recvData, ts.config.MaxUdpDatagramBuffered)}
	us.SetReadDeadline(zeroTime)

	if isListener {
		us.sessKeyToSessAddr = make(map[string]*udpSessionAddr)
		us.udpAddrToSessAddr = make(map[string]*udpSessionAddr)
		us.tcpConns = make(map[string]*Conn)
	}

	return us
}

func (us *UdpSession) DialUpSession(listenerNknAddr string, sessionID []byte, config *nkn.DialConfig) (err error) {
	if us.ts.isClosed {
		return ErrClosed
	}

	ctx := context.Background()
	var cancel context.CancelFunc
	if config.DialTimeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, time.Duration(config.DialTimeout)*time.Millisecond)
		defer cancel()
	}

	pubAddrs, err := us.ts.getPubAddrsFromRemote(ctx, listenerNknAddr, sessionID)
	if err != nil {
		return err
	}

	udpConn, pubaddr, err := us.dialUdpConn(pubAddrs)
	if err != nil {
		return err
	}

	tcpConn, err := us.ts.dialTcpConn(ctx, listenerNknAddr, sessionID, pb.SessionType_UDP, -1, pubaddr, config)
	if err != nil {
		udpConn.Close()
		return err
	}

	udpAddr := &net.UDPAddr{IP: net.ParseIP(pubaddr.IP), Port: int(pubaddr.Port)}
	usa := newUdpSessionAddr(sessionID, listenerNknAddr, udpAddr)

	us.Lock()
	us.udpConn = udpConn
	us.tcpConn = tcpConn
	us.peer = usa
	us.Unlock()

	err = us.sendMetaData(sessionID)
	if err != nil {
		us.Close()
		return err
	}

	err = us.addCodec(udpAddr, listenerNknAddr)
	if err != nil {
		us.Close()
		return err
	}

	return nil
}

func (us *UdpSession) dialUdpConn(pubAddrs *PubAddrs) (udpConn *tuna.EncryptUDPConn, addr *PubAddr, err error) {
	var conn *net.UDPConn
	for _, addr = range pubAddrs.Addrs {
		if len(addr.IP) > 0 && addr.Port > 0 {
			udpAddr := &net.UDPAddr{IP: net.ParseIP(addr.IP), Port: int(addr.Port)}
			conn, err = net.DialUDP("udp", nil, udpAddr)
			if err != nil {
				log.Printf("dial udp %v:%v err: %v", addr.IP, addr.Port, err)
				continue
			}
			break
		}
	}
	if conn == nil {
		return nil, nil, err
	}

	udpConn = tuna.NewEncryptUDPConn(conn)
	return udpConn, addr, nil
}

func (us *UdpSession) addCodec(udpAddr *net.UDPAddr, remoteAddr string) error {
	remotePublicKey, err := nkn.ClientAddrToPubKey(remoteAddr)
	if err != nil {
		return err
	}
	sharedKey, err := us.ts.getOrComputeSharedKey(remotePublicKey)
	if err != nil {
		return err
	}

	conn, err := us.GetEstablishedUDPConn()
	if err != nil {
		return err
	}
	err = conn.AddCodec(udpAddr, sharedKey, tpb.EncryptionAlgo_ENCRYPTION_XSALSA20_POLY1305, false)
	if err != nil {
		return err
	}

	return nil
}

// ReadFrom, implement of standard library function: net.UDPConn.ReadFrom
func (us *UdpSession) ReadFrom(b []byte) (n int, addr net.Addr, err error) {
	if us.IsClosed() {
		return 0, nil, ErrClosed
	}

	var msg = &recvData{}
	select {
	case msg = <-us.recvChan:
		n = copy(b, msg.data)

		us.Lock()
		us.recvSize = us.recvSize - len(msg.data)
		us.Unlock()

		addr = msg.usa
	case <-us.readContext.Done():
		err = us.readContext.Err()
	}

	return
}

func (us *UdpSession) GetUDPConn() *tuna.EncryptUDPConn {
	us.RLock()
	defer us.RUnlock()
	return us.udpConn
}

func (us *UdpSession) GetEstablishedUDPConn() (*tuna.EncryptUDPConn, error) {
	if us.ts.isClosed {
		return nil, ErrClosed
	}
	conn := us.GetUDPConn()
	if conn == nil {
		return nil, ErrNilConn
	}
	if conn.IsClosed() {
		return nil, ErrClosed
	}
	return conn, nil
}

// Loop receiving msg from udp.
func (us *UdpSession) recvMsg() (err error) {
	buf := make([]byte, tuna.MaxUDPBufferSize)
	for {
		conn, err := us.GetEstablishedUDPConn()
		if err != nil {
			return err
		}

		n, udpAddr, encrypted, err := conn.ReadFromUDPEncrypted(buf)
		if err != nil {
			continue
		}

		header := buf[0]
		switch header {
		case byte(pb.HeaderType_USER):
			if !encrypted { // should not receive un-encryted user data
				continue
			}

			var usa *udpSessionAddr
			var ok bool
			if us.isListener {
				us.RLock()
				usa, ok = us.udpAddrToSessAddr[udpAddr.String()]
				us.RUnlock()
			} else {
				ok = udpAddr.String() == us.peer.udpAddr.String()
			}

			if ok { // only accept the data which has set up session
				us.Lock()
				if us.recvSize+n-1 < us.ts.config.UDPRecvBufferSize {
					b := make([]byte, n-1)
					copy(b, buf[1:n]) // remove header, and return to user application
					select {
					case us.recvChan <- &recvData{data: b, usa: usa}:
						us.recvSize += n - 1
					default:
					}
				}
				us.Unlock()
			} else {
				log.Printf("UdpSession.recvMsg addr %v is not in session, but got user data len %v", udpAddr, n)
			}

		case byte(pb.HeaderType_SESSION):
			err := us.handleMetaData(buf[1:n], udpAddr)
			if err != nil {
				log.Printf("UdpSession.recvMsg handleMetaData error %v", err)
			}

		default:
			log.Printf("UdpSession.recvMsg unknown format message")
		}
	}
}

func (us *UdpSession) Read(b []byte) (int, error) {
	n, _, err := us.ReadFrom(b)
	return n, err
}

// ReadFrom, implement of standard library net.UDPConn.WriteTo
// Only for user data write
func (us *UdpSession) WriteTo(b []byte, addr net.Addr) (n int, err error) {
	buf := make([]byte, 1+len(b))
	buf[0] = byte(pb.HeaderType_USER)
	copy(buf[1:], b)

	var udpAddr *net.UDPAddr
	if us.isListener { // only listener need to use udpAddr
		udpAddr, err = us.getUdpAddr(addr)
		if err != nil {
			return 0, err
		}
	}

	conn, err := us.GetEstablishedUDPConn()
	if err != nil {
		return 0, err
	}
	n, _, err = conn.WriteMsgUDP(buf, nil, udpAddr)
	if err != nil {
		return 0, err
	}

	return n - 1, nil
}

func (us *UdpSession) Write(b []byte) (int, error) {
	if us.isListener {
		return 0, ErrOnlyDailer
	}
	return us.WriteTo(b, us.peer)
}

func (us *UdpSession) SetDeadline(t time.Time) error {
	if us.ts.isClosed {
		return ErrClosed
	}

	conn, err := us.GetEstablishedUDPConn()
	if err != nil {
		return err
	}
	return conn.SetDeadline(t)
}

func (us *UdpSession) SetReadDeadline(t time.Time) error {
	if t == zeroTime {
		us.readContext, us.readCancel = context.WithCancel(context.Background())
	} else {
		us.readContext, us.readCancel = context.WithDeadline(context.Background(), t)
	}

	conn, err := us.GetEstablishedUDPConn()
	if err != nil {
		return err
	}
	return conn.SetReadDeadline(t)
}

func (us *UdpSession) SetWriteDeadline(t time.Time) error {
	conn, err := us.GetEstablishedUDPConn()
	if err != nil {
		return err
	}
	return conn.SetWriteDeadline(t)
}

func (us *UdpSession) SetWriteBuffer(size int) error {
	conn, err := us.GetEstablishedUDPConn()
	if err != nil {
		return err
	}
	return conn.SetWriteBuffer(size)
}

func (us *UdpSession) SetReadBuffer(size int) error {
	conn, err := us.GetEstablishedUDPConn()
	if err != nil {
		return err
	}
	return conn.SetReadBuffer(size)
}

func (us *UdpSession) IsClosed() bool {
	_, err := us.GetEstablishedUDPConn()
	if err != nil {
		return true
	}
	return false
}

func (us *UdpSession) LocalAddr() net.Addr {
	conn, err := us.GetEstablishedUDPConn()
	if err != nil {
		return nil
	}
	return conn.LocalAddr()
}

func (us *UdpSession) RemoteAddr() net.Addr {
	if us.isListener {
		return nil
	}

	us.RLock()
	defer us.RUnlock()
	if us.peer != nil {
		return us.peer.udpAddr
	} else {
		return nil
	}
}

// The endpoint close session actively.
func (us *UdpSession) Close() (err error) {
	us.sendCloseMsg(nil)

	if conn, err := us.GetEstablishedUDPConn(); err == nil {
		conn.Close()
	}

	us.Lock()
	defer us.Unlock()
	if us.isListener {
		us.sessKeyToSessAddr = make(map[string]*udpSessionAddr)
		us.udpAddrToSessAddr = make(map[string]*udpSessionAddr)
		for _, conn := range us.tcpConns {
			if conn != nil {
				conn.Close()
			}
		}
		us.tcpConns = make(map[string]*Conn)
	} else {
		us.peer = nil
		if us.tcpConn != nil {
			return us.tcpConn.Close()
		}
	}

	return
}

func (us *UdpSession) sendCloseMsg(udpAddr *net.UDPAddr) (err error) {
	b, err := newSessionData(pb.MsgType_CLOSE, nil)
	if err != nil {
		return err
	}

	us.RLock()
	defer us.RUnlock()

	if !us.isListener && us.tcpConn != nil { // Dialer
		_, err = us.tcpConn.Write(b)
		return
	}

	if udpAddr != nil { // Send close message to this dialer address
		if usa, ok := us.udpAddrToSessAddr[udpAddr.String()]; ok {
			if tcpConn, ok := us.tcpConns[usa.String()]; ok {
				_, err = tcpConn.Write(b)
			}
		}
		return
	}

	for _, usa := range us.sessKeyToSessAddr { // Send close message to all dialers.
		if tcpConn, ok := us.tcpConns[usa.String()]; ok {
			_, err = tcpConn.Write(b)
		}
	}

	return
}

// Handle connection closed by peer or getting closed message from peer.
func (us *UdpSession) handleConnClosed(sessKey string) {
	us.Lock()
	defer us.Unlock()
	if us.isListener {
		if usa, ok := us.sessKeyToSessAddr[sessKey]; ok {
			delete(us.sessKeyToSessAddr, sessKey)
			delete(us.udpAddrToSessAddr, usa.udpAddr.String())
			if tcpConn, ok := us.tcpConns[sessKey]; ok {
				tcpConn.Close()
				delete(us.tcpConns, sessKey)
			}
		}
	} else {
		us.peer = nil
		us.udpConn.Close()
		us.tcpConn.Close()
	}
}

// Get UDPAddr by net.Addr
func (us *UdpSession) getUdpAddr(addr net.Addr) (udpAddr *net.UDPAddr, err error) {
	us.RLock()
	defer us.RUnlock()

	if !us.isListener {
		udpAddr = us.peer.udpAddr
		return
	}
	// for listener
	if addr == nil {
		return nil, ErrWrongAddr
	} else {
		usa, ok := addr.(*udpSessionAddr) // addr should be *udpSessionAddr
		if !ok {
			return nil, ErrWrongAddr
		}

		usa, ok = us.sessKeyToSessAddr[usa.String()]
		if ok {
			udpAddr = usa.udpAddr
		} else {
			return nil, ErrWrongAddr
		}
	}

	return
}

func (us *UdpSession) sendMetaData(sessionID []byte) (err error) {
	metadata := &pb.SessionMetadata{
		Id:            sessionID,
		SessionType:   pb.SessionType_UDP,
		DialerNknAddr: us.ts.Address(),
	}
	buf, err := proto.Marshal(metadata)
	if err != nil {
		return err
	}

	b, err := newSessionData(pb.MsgType_SESSIONID, buf)
	if err != nil {
		return err
	}

	conn, err := us.GetEstablishedUDPConn()
	if err != nil {
		return err
	}
	_, _, err = conn.WriteMsgUDP(b, nil, nil)

	return err
}

// Handle session meta data.
func (us *UdpSession) handleMetaData(b []byte, udpAddr *net.UDPAddr) error {
	if !us.isListener { // dialer should not get session meta data.
		return nil
	}

	sessData, err := parseSessionData(b)
	if err != nil {
		return err
	}

	switch sessData.MsgType {
	case pb.MsgType_SESSIONID:
		metadata := &pb.SessionMetadata{}
		err = proto.Unmarshal(sessData.Data, metadata)
		if err != nil {
			return err
		}
		usa := &udpSessionAddr{sessionID: metadata.Id, remoteNknAddr: metadata.DialerNknAddr, udpAddr: udpAddr}
		if _, ok := us.sessKeyToSessAddr[usa.String()]; ok { // This session is exist already
			return nil
		}

		us.Lock()
		us.sessKeyToSessAddr[usa.String()] = usa
		us.udpAddrToSessAddr[udpAddr.String()] = usa
		us.Unlock()
		us.addCodec(udpAddr, metadata.DialerNknAddr) // listener add codec

	case pb.MsgType_CLOSE:
		if usa, ok := us.udpAddrToSessAddr[udpAddr.String()]; ok {
			us.handleConnClosed(usa.String())
		}

	default:
		return ErrWrongMsgFormat
	}

	return nil
}

// New session data
func newSessionData(msgType pb.MsgType, data []byte) ([]byte, error) {
	sessData := pb.SessionData{MsgType: msgType, Data: data}
	buf, err := proto.Marshal(&sessData)
	if err != nil {
		return nil, err
	}
	b := append([]byte{byte(pb.HeaderType_SESSION)}, buf...)

	return b, err
}

// Parse buffer to message type and data
func parseSessionData(b []byte) (sessData *pb.SessionData, err error) {
	var sd pb.SessionData
	err = proto.Unmarshal(b, &sd)
	if err != nil {
		return nil, err
	}

	return &sd, nil
}

func (us *UdpSession) handleTcpMsg(conn *Conn, sessKey string) error {
	tcpConn := conn.Conn.(*net.TCPConn)
	err := tcpConn.SetKeepAlive(true)
	if err != nil {
		log.Printf("udpStatusMonitor SetKeepAlive %v", err)
	}

	b := make([]byte, 1024)
	for {
		_, err := us.GetEstablishedUDPConn()
		if err != nil {
			return err
		}

		n, err := tcpConn.Read(b)
		if err != nil {
			return err
		}
		if b[0] != byte(pb.HeaderType_SESSION) {
			continue
		}

		sessData, err := parseSessionData(b[1:n])
		if err != nil {
			log.Printf("UdpSession handleTcpMsg parseSessionData err %v", err)
			continue
		}
		switch sessData.MsgType {
		case pb.MsgType_CLOSE:
			us.handleConnClosed(sessKey)
			return ErrClosed

		default:
		}
	}
}

// for test only.
func (us *UdpSession) CloseTcp(i int) {
	us.RLock()
	defer us.RUnlock()
	log.Printf("Dialer %v is going to close TCP for testing\n", i)
	us.tcpConn.Close()
}
