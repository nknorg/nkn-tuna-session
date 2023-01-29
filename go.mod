module github.com/nknorg/nkn-tuna-session

go 1.19

// replace github.com/nknorg/tuna => /Users/bufrr/github/tuna

require (
	github.com/imdario/mergo v0.3.13
	github.com/nknorg/ncp-go v1.0.4-0.20220224111535-206abfb10fe8
	github.com/nknorg/nkn-sdk-go v1.4.2-0.20220913025957-d204cd062fd4
	github.com/nknorg/nkn/v2 v2.1.8
	github.com/nknorg/nkngomobile v0.0.0-20220615081414-671ad1afdfa9
	github.com/nknorg/tuna v0.0.0-20220401064644-3907cf1cd3a2
	github.com/patrickmn/go-cache v2.1.0+incompatible
	golang.org/x/crypto v0.0.0-20220525230936-793ad666bf5e
	google.golang.org/protobuf v1.27.1
)

require (
	github.com/golang/protobuf v1.5.2 // indirect
	github.com/gorilla/websocket v1.4.2 // indirect
	github.com/hashicorp/errwrap v1.0.0 // indirect
	github.com/hashicorp/go-multierror v1.0.0 // indirect
	github.com/itchyny/base58-go v0.0.5 // indirect
	github.com/jpillora/backoff v1.0.0 // indirect
	github.com/nknorg/encrypted-stream v1.0.1 // indirect
	github.com/oschwald/geoip2-golang v1.4.0 // indirect
	github.com/oschwald/maxminddb-golang v1.6.0 // indirect
	github.com/pbnjay/memory v0.0.0-20190104145345-974d429e7ae4 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/rdegges/go-ipify v0.0.0-20150526035502-2d94a6a86c40 // indirect
	github.com/xtaci/smux v2.0.1+incompatible // indirect
	golang.org/x/sys v0.0.0-20220804214406-8e32c043e418 // indirect
)

replace github.com/nknorg/tuna v0.0.0-20220401064644-3907cf1cd3a2 => github.com/nknorg/tuna v0.0.0-20230128070346-2e1102d13fcb
