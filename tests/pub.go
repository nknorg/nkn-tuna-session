package tests

import (
	"encoding/hex"
	"log"

	nkn "github.com/nknorg/nkn-sdk-go"
	ts "github.com/nknorg/nkn-tuna-session"
)

func CreateAccountAndWallet(seedHex string) (acc *nkn.Account, wal *nkn.Wallet, err error) {
	seed, err := hex.DecodeString(seedHex)
	if err != nil {
		log.Fatal(err)
		return nil, nil, err
	}

	acc, err = nkn.NewAccount(seed)
	if err != nil {
		log.Fatal(err)
		return
	}

	wal, err = nkn.NewWallet(acc, nil)
	if err != nil {
		log.Fatal(err)
	}
	bal, _ := wal.Balance()
	log.Printf("wallet address is %v, balance is %v", wal.Address(), bal)

	return
}

func CreateTunaSessionConfig(numListener int) (config *ts.Config) {
	config = &ts.Config{
		NumTunaListeners: numListener,
		TunaMaxPrice:     "0.01",
	}
	return config
}

func CreateDialConfig(timeout int32) (config *nkn.DialConfig) {
	config = &nkn.DialConfig{DialTimeout: timeout}
	return
}

func CreateClientConfig(retries int32) (config *nkn.ClientConfig) {
	config = &nkn.ClientConfig{ConnectRetries: retries}
	return
}

func CreateMultiClient(account *nkn.Account, id string, numClient int) (mc *nkn.MultiClient, err error) {
	clientConfig := CreateClientConfig(1)
	mc, err = nkn.NewMultiClient(account, id, numClient, false, clientConfig)
	if err != nil {
		log.Fatal(err)
	}

	<-mc.OnConnect.C
	return
}

func CreateTunaSession(account *nkn.Account, wallet *nkn.Wallet, mc *nkn.MultiClient, numListener int) (tunaSess *ts.TunaSessionClient, err error) {
	config := CreateTunaSessionConfig(numListener)
	tunaSess, err = ts.NewTunaSessionClient(account, mc, wallet, config)
	if err != nil {
		log.Fatal(err)
	}
	return
}
