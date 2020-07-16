package main

import (
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

	ncp "github.com/nknorg/ncp-go"
	nkn "github.com/nknorg/nkn-sdk-go"
	ts "github.com/nknorg/nkn-tuna-session"
	"github.com/nknorg/tuna"
)

const (
	dialID   = "alice"
	listenID = "bob"
)

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
	mtu := flag.Int("mtu", 0, "ncp session mtu")

	flag.Parse()

	*numBytes *= 1 << 20

	seed, err := hex.DecodeString(*seedHex)
	if err != nil {
		log.Fatal(err)
	}

	countries := strings.Split(*tunaCountry, ",")
	locations := make([]tuna.Location, len(countries))
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
	dialConfig := &ts.DialConfig{DialTimeout: 5000}
	config := &ts.Config{
		NumTunaListeners:       *numTunaListeners,
		TunaServiceName:        *tunaServiceName,
		TunaSubscriptionPrefix: *tunaSubscriptionPrefix,
		TunaIPFilter:           &tuna.IPFilter{Allow: locations},
		SessionConfig:          &ncp.Config{MTU: int32(*mtu)},
		TunaMaxPrice:           "0.01",
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

	if *dial {
		m, err := nkn.NewMultiClient(account, dialID, *numClients, false, clientConfig)
		if err != nil {
			log.Fatal(err)
		}

		<-m.OnConnect.C
		time.Sleep(time.Second)

		c, err := ts.NewTunaSessionClient(account, m, wallet, config)
		if err != nil {
			log.Fatal(err)
		}

		if len(*dialAddr) == 0 {
			*dialAddr = listenID + "." + strings.SplitN(c.Addr().String(), ".", 2)[1]
		}

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

	select {}
}
