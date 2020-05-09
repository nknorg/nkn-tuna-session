# NKN Tuna Session

NKN Tuna Session is an overlay peer to peer connection based on multiple
concurrent [tuna](https://github.com/nknorg/tuna) connections and
[ncp](https://github.com/nknorg/ncp-go) protocol.

A few feature highlights:

* Performance: Use multiple parallel paths to boost overall throughput.

* Network agnostic: Neither dialer nor listener needs to have public IP address
  or NAT traversal. They are guaranteed to be connected regardless of their
  network conditions.

* Security: Using public key as address, which enables built-in end to end
  encryption while being invulnerable to man-in-the-middle attack.

A simple illustration of a session between Alice and Bob:

```
        X
      /   \
Alice - Y - Bob
      \   /
        Z
```

Listener (Bob for example) will pay relayers in the middle (X, Y, Z) for
relaying the traffic using NKN tokens. The payment will be based on bandwidth
usage of the session.

## Usage

You first need to import both `nkn-sdk-go` and `nkn-tuna-session`:

```go
import (
	nkn "github.com/nknorg/nkn-sdk-go"
	ts "github.com/nknorg/nkn-tuna-session"
)
```

Create a multi-client and wallet (see
[nkn-sdk-go](https://github.com/nknorg/nkn-sdk-go) for details) or re-use your
existing ones:

```go
multiclient, err := nkn.NewMultiClient(...)
// wallet is only needed for listener side
wallet, err := nkn.NewWallet(...)
```

Then you can create a tuna session client:

```go
// wallet is only needed for listener side and will be used for payment
// price is in unit of NKN token per MB
c, err := ts.NewTunaSessionClient(account, multiclient, wallet, &ts.Config{TunaMaxPrice: "0"})
```

A tuna session client can start listening for incoming session where the remote
address match any of the given regexp:

```go
// Accepting any address, equivalent to c.Listen(nkn.NewStringArray(".*"))
err = c.Listen(nil)
// Only accepting pubkey 25d660916021ab1d182fb6b52d666b47a0f181ed68cf52a056041bdcf4faaf99 but with any identifiers
err = c.Listen(nkn.NewStringArray("25d660916021ab1d182fb6b52d666b47a0f181ed68cf52a056041bdcf4faaf99$"))
// Only accepting address alice.25d660916021ab1d182fb6b52d666b47a0f181ed68cf52a056041bdcf4faaf99
err = c.Listen(nkn.NewStringArray("^alice\\.25d660916021ab1d182fb6b52d666b47a0f181ed68cf52a056041bdcf4faaf99$"))
```

Then it can start accepting sessions:

```go
session, err := c.Accept()
```

Tuna session client implements `net.Listener` interface, so one can use it as a
drop-in replacement when `net.Listener` is needed, e.g. `http.Serve`.

On the other hand, any tuna session client can dial a session to a remote NKN
address:

```go
session, err := c.Dial("another nkn address")
```

Session implements `net.Conn` interface, so it can be used as a drop-in
replacement when `net.Conn` is needed:

```go
buf := make([]byte, 1024)
n, err := session.Read(buf)
n, err := session.Write(buf)
```

A few more complicated examples can be found at [examples](examples)

## Usage on iOS/Android

This library is designed to work with
[gomobile](https://godoc.org/golang.org/x/mobile/cmd/gomobile) and run natively
on iOS/Android without any modification. You can use `gomobile bind` to compile
it to Objective-C framework for iOS:

```shell
gomobile bind -target=ios -ldflags "-s -w" github.com/nknorg/nkn-tuna-session github.com/nknorg/nkn-sdk-go github.com/nknorg/ncp-go github.com/nknorg/tuna
```

and Java AAR for Android:

```shell
gomobile bind -target=android -ldflags "-s -w" github.com/nknorg/nkn-tuna-session github.com/nknorg/nkn-sdk-go github.com/nknorg/ncp-go github.com/nknorg/tuna
```

## Contributing

**Can I submit a bug, suggestion or feature request?**

Yes. Please open an issue for that.

**Can I contribute patches?**

Yes, we appreciate your help! To make contributions, please fork the repo, push
your changes to the forked repo with signed-off commits, and open a pull request
here.

Please sign off your commit. This means adding a line "Signed-off-by: Name
<email>" at the end of each commit, indicating that you wrote the code and have
the right to pass it on as an open source patch. This can be done automatically
by adding -s when committing:

```shell
git commit -s
```

## Community

* [Discord](https://discord.gg/c7mTynX)
* [Telegram](https://t.me/nknorg)
* [Reddit](https://www.reddit.com/r/nknblockchain/)
* [Twitter](https://twitter.com/NKN_ORG)
