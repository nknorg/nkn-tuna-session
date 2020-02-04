// Basic usage: main -l -d -n 4
// Use --help to see more options

package main

import (
	"encoding/hex"
	"flag"
	"io"
	"log"
	"net"
	"net/http"
	"strings"

	nkn "github.com/nknorg/nkn-sdk-go"
	ts "github.com/nknorg/nkn-tuna-session"
)

const (
	dialID   = "alice"
	listenID = "bob"
)

func main() {
	numTunaListeners := flag.Int("n", 1, "number of tuna listeners")
	numClients := flag.Int("c", 4, "number of clients")
	seedHex := flag.String("s", "", "secret seed")
	dialAddr := flag.String("a", "", "dial address")
	dial := flag.Bool("d", false, "dial")
	listen := flag.Bool("l", false, "listen")
	serveDir := flag.String("dir", ".", "serve directory")
	httpAddr := flag.String("http", ":8080", "http listen address")

	flag.Parse()

	seed, err := hex.DecodeString(*seedHex)
	if err != nil {
		log.Fatal(err)
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
	config := &ts.Config{NumTunaListeners: *numTunaListeners}

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

		go func() {
			log.Println("Serving content at", m.Addr().String())
			fs := http.FileServer(http.Dir(*serveDir))
			http.Handle("/", fs)
			http.Serve(c, nil)
		}()
	}

	if *dial {
		m, err := nkn.NewMultiClient(account, dialID, *numClients, false, clientConfig)
		if err != nil {
			log.Fatal(err)
		}

		<-m.OnConnect.C

		c, err := ts.NewTunaSessionClient(account, m, wallet, config)
		if err != nil {
			log.Fatal(err)
		}

		if len(*dialAddr) == 0 {
			*dialAddr = listenID + "." + strings.SplitN(m.Addr().String(), ".", 2)[1]
		}

		listener, err := net.Listen("tcp", *httpAddr)
		if err != nil {
			log.Fatal(err)
		}

		log.Println("Http server listening at", *httpAddr)

		go func() {
			for {
				conn, err := listener.Accept()
				if err != nil {
					log.Fatal(err)
				}

				sess, err := c.Dial(*dialAddr)
				if err != nil {
					log.Fatal(err)
				}

				go func() {
					io.Copy(conn, sess)
				}()
				go func() {
					io.Copy(sess, conn)
				}()
			}
		}()
	}

	select {}
}
