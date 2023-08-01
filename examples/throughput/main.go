package main

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"math"
	"net"
	"os"
	"strings"
	"time"

	"github.com/nknorg/ncp-go"
	"github.com/nknorg/nkn-sdk-go"
	ts "github.com/nknorg/nkn-tuna-session"
	"github.com/nknorg/tuna/geo"
)

const (
	dialID   = "alice"
	listenID = "bob"
)

var udpBytesReceived = 0
var udpBytesSent = 0

func read(sess net.Conn) error {
	timeStart := time.Now()

	b := make([]byte, 4)
	n := 0
	for {
		m, err := sess.Read(b[n:])
		if err != nil {
			return err
		}
		n += m
		if n == 4 {
			break
		}
	}

	numBytes := int(binary.LittleEndian.Uint32(b))

	b = make([]byte, 1024)
	bytesReceived := 0
	for {
		n, err := sess.Read(b)
		if err != nil {
			return err
		}
		for i := 0; i < n; i++ {
			if b[i] != byte(bytesReceived%256) {
				return fmt.Errorf("byte %d should be %d, got %d", bytesReceived, bytesReceived%256, b[i])
			}
			bytesReceived++
		}
		if ((bytesReceived - n) * 10 / numBytes) != (bytesReceived * 10 / numBytes) {
			log.Println("Received", bytesReceived, "bytes", float64(bytesReceived)/math.Pow(2, 20)/(float64(time.Since(timeStart))/float64(time.Second)), "MB/s")
		}
		if bytesReceived == numBytes {
			log.Println("Finished receiving", bytesReceived, "bytes")
			return nil
		}
	}
}

func write(sess net.Conn, numBytes int) error {
	timeStart := time.Now()

	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, uint32(numBytes))
	_, err := sess.Write(b)
	if err != nil {
		return err
	}

	bytesSent := 0
	for i := 0; i < numBytes/1024; i++ {
		b := make([]byte, 1024)
		for j := 0; j < len(b); j++ {
			b[j] = byte(bytesSent % 256)
			bytesSent++
		}
		n, err := sess.Write(b)
		if err != nil {
			return err
		}
		if n != len(b) {
			return fmt.Errorf("sent %d instead of %d bytes", n, len(b))
		}
		if ((bytesSent - n) * 10 / numBytes) != (bytesSent * 10 / numBytes) {
			log.Println("Sent", bytesSent, "bytes", float64(bytesSent)/math.Pow(2, 20)/(float64(time.Since(timeStart))/float64(time.Second)), "MB/s")
		}
	}
	return nil
}

func readUDP(conn *ts.UdpSession, numBytes int) error {
	defer conn.Close()
	buffer := make([]byte, 1024)
	var timeStart time.Time
	defer func() {
		if udpBytesReceived > 0 {
			mbTobytes := math.Pow(2, 20)
			sent := float64(udpBytesSent) / mbTobytes
			received := float64(udpBytesReceived) / mbTobytes
			speed := received / time.Since(timeStart).Seconds()
			log.Printf("UDP: Received %.2f MB bytes, speed: %.2f MB/s, package loss:  %.2f%% \n", received, speed, 100*(1-received/sent))
		}
	}()
	for {
		err := conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		if err != nil {
			return err
		}
		n, _, err := conn.ReadFrom(buffer)
		if udpBytesReceived == 0 {
			timeStart = time.Now()
		}
		if err != nil {
			return err
		}
		udpBytesReceived += n
		if ((udpBytesReceived - n) * 10 / numBytes) != (udpBytesReceived * 10 / numBytes) {
			mbTobytes := math.Pow(2, 20)
			received := float64(udpBytesReceived) / mbTobytes
			speed := received / time.Since(timeStart).Seconds()
			log.Printf("UDP: Received %.2f MB bytes, speed: %.2f MB/s\n", received, speed)
		}
		if udpBytesReceived >= numBytes {
			return nil
		}
	}
}

func writeUDP(conn *ts.UdpSession, numBytes int) error {
	defer conn.Close()
	buffer := make([]byte, 1024)
	timeStart := time.Now()
	for {
		rand.Read(buffer)
		n, err := conn.WriteTo(buffer, nil)
		if err != nil {
			return err
		}
		udpBytesSent += n
		if ((udpBytesSent - n) * 10 / numBytes) != (udpBytesSent * 10 / numBytes) {
			mbTobytes := math.Pow(2, 20)
			sent := float64(udpBytesSent) / mbTobytes
			speed := sent / time.Since(timeStart).Seconds()
			log.Printf("UDP: Sent %.2f MB bytes, speed: %.2f MB/s\n", sent, speed)
		}
		if udpBytesSent >= numBytes {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	return nil
}

func main() {
	numTunaListeners := flag.Int("n", 1, "number of tuna listeners")
	numBytes := flag.Int("m", 1, "data to send (MB)")
	numClients := flag.Int("c", 4, "number of clients")
	seedHex := flag.String("s", "", "secret seed")
	dialAddr := flag.String("a", "", "dial address")
	dial := flag.Bool("d", false, "dial")
	listen := flag.Bool("l", false, "listen")
	tunaCountry := flag.String("country", "", `tuna service node allowed country code, separated by comma, e.g. "US" or "US,CN"`)
	tunaServiceName := flag.String("tsn", "", "tuna reverse service name")
	tunaSubscriptionPrefix := flag.String("tsp", "", "tuna subscription prefix")
	tunaMeasureBandwidth := flag.Bool("tmb", false, "tuna measure bandwidth")
	tunaMeasurementBytesDownLink := flag.Int("tmbd", 1, "tuna measure bandwidth downlink in bytes")
	tunaMaxPrice := flag.String("price", "0.01", "tuna reverse service max price in unit of NKN/MB")
	mtu := flag.Int("mtu", 0, "ncp session mtu")
	u := flag.Bool("u", false, "send data through UDP instead TCP")

	flag.Parse()

	*numBytes *= 1 << 20

	seed, err := hex.DecodeString(*seedHex)
	if err != nil {
		log.Fatal(err)
	}

	countries := strings.Split(*tunaCountry, ",")
	locations := make([]geo.Location, len(countries))
	for i := range countries {
		locations[i].CountryCode = strings.TrimSpace(countries[i])
	}

	account, err := nkn.NewAccount(seed)
	if err != nil {
		log.Fatal(err)
	}

	wallet, err := nkn.NewWallet(account, nil)
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Seed:", hex.EncodeToString(account.Seed()))

	clientConfig := &nkn.ClientConfig{ConnectRetries: 1}
	dialConfig := &nkn.DialConfig{DialTimeout: 5000}
	config := &ts.Config{
		NumTunaListeners:             *numTunaListeners,
		TunaServiceName:              *tunaServiceName,
		TunaSubscriptionPrefix:       *tunaSubscriptionPrefix,
		TunaMeasureBandwidth:         *tunaMeasureBandwidth,
		TunaMeasurementBytesDownLink: int32(*tunaMeasurementBytesDownLink),
		TunaMaxPrice:                 *tunaMaxPrice,
		TunaIPFilter:                 &geo.IPFilter{Allow: locations},
		SessionConfig:                &ncp.Config{MTU: int32(*mtu)},
	}

	if *listen {
		m, err := nkn.NewMultiClient(account, listenID, *numClients, false, clientConfig)
		if err != nil {
			log.Fatal(err)
		}

		<-m.OnConnect.C

		c, err := ts.NewTunaSessionClient(account, m, wallet, config)
		if err != nil {
			log.Fatal(err)
		}

		if *u {
			uConn, err := c.ListenUDP()
			if err != nil {
				log.Fatal(err)
			}
			log.Println("Listening UDP at", c.Addr())
			go func() {
				err = readUDP(uConn, *numBytes)
				if err != nil {
					log.Fatal(err)
				}
				os.Exit(0)
			}()
		} else {
			err = c.Listen(nil)
			if err != nil {
				log.Fatal(err)
			}
			log.Println("Listening at", c.Addr())
			go func() {
				for {
					s, err := c.Accept()
					if err != nil {
						log.Fatal(err)
					}
					log.Println(c.Addr(), "accepted a session")

					go func(s net.Conn) {
						err := read(s)
						if err != nil {
							log.Fatal(err)
						}
						s.Close()
					}(s)
				}
			}()
		}
		<-c.OnConnect()
	}

	if *dial {
		m, err := nkn.NewMultiClient(account, dialID, *numClients, false, clientConfig)
		if err != nil {
			log.Fatal(err)
		}

		<-m.OnConnect.C
		time.Sleep(10 * time.Second)

		c, err := ts.NewTunaSessionClient(account, m, wallet, config)
		if err != nil {
			log.Fatal(err)
		}

		if len(*dialAddr) == 0 {
			*dialAddr = listenID + "." + strings.SplitN(c.Addr().String(), ".", 2)[1]
		}

		if *u {
			udpConn, err := c.DialUDPWithConfig(*dialAddr, dialConfig)
			if err != nil {
				log.Fatal(err)
			}
			log.Println(c.Addr(), "dialed a UDP session")
			go func() {
				err := writeUDP(udpConn, *numBytes)
				if err != nil {
					log.Fatal(err)
				}
			}()
		} else {
			s, err := c.DialWithConfig(*dialAddr, dialConfig)
			if err != nil {
				log.Fatal(err)
			}
			log.Println(c.Addr(), "dialed a session")

			go func() {
				err := write(s, *numBytes)
				if err != nil {
					log.Fatal(err)
				}
				for {
					if s.IsClosed() {
						os.Exit(0)
					}
					time.Sleep(time.Millisecond * 100)
				}
			}()
		}
	}

	select {}
}
