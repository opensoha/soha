# Go storage driver SDK

Implement `storagedriver.Driver` and serve it from the plugin entry point. The SDK is part of the `github.com/opensoha/soha/contracts` Go module and adds only standard-library runtime imports. See the [storage RPC contract](../README.md).

```go
var version = "0.0.0-dev"

func main() {
    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    defer stop()
    if err := sdk.Serve(ctx, os.Stdin, sdk.ServerOptions{
        PluginID: "example.storage", Version: version,
    }, newDriver()); err != nil {
        log.Fatal(err)
    }
}
```

Import `github.com/opensoha/soha/contracts/plugins/storagedriver/sdk`. Build with `-ldflags="-X main.version=<manifest version>"`. The driver obtains its identity from its own build. The supervisor obtains the expected identity from the verified plugin package.

The supervisor sends one `sdk.Bootstrap` through a private child stdin pipe using `sdk.WriteBootstrap` and closes the pipe. The supported startup configurations are:

- `Network: "unix"` with an absolute socket path under an existing, protected parent directory. The SDK rejects world-accessible parents and does not replace an existing socket. The socket defaults to `0600`. The supervisor can select `0660` with a group limited to the host and driver identities.
- `Network: "tcp"` with a literal loopback address and nonzero port. Supply a fresh TLS certificate in DER and its unencrypted PKCS#8 private key in DER using `sdk.TLSBootstrap`. The host supplies the corresponding certificate DER SHA-256 to `sdk.Dial`. The connection uses TLS 1.3 and verifies the exact certificate before sending the HTTP handshake.

Supply a fresh random 32-byte `LaunchSecret` for either transport. Bootstrap JSON is limited to 64 KiB and encodes byte fields as standard padded base64. Keep the bootstrap and session credentials out of environment variables, command-line arguments, files and logs. The caller retains ownership of its launch secret and should erase it after `sdk.Dial`. OS process identity, socket directory ownership and isolation belong to the supervisor.

The host uses `sdk.Dial(ctx, sdk.ClientOptions{...})` after the endpoint is listening. `Dial` performs one handshake and returns a `*sdk.Client` implementing `storagedriver.Driver`. A failed or ambiguous handshake requires retiring that launch. The host closes the client when retiring the process. Client and server call deadlines default to five minutes and can be overridden via `CallTimeout`. Calls use separate connections to prevent transparent replay after a connection failure. The client deadline covers the returned Open body until EOF or `Close`. A driver should observe `ctx` during provider operations. A blocking upload reader should implement `io.ReadCloser` so cancellation can interrupt it. Both upload readers and driver Open bodies must support `Close` while a read is pending. The SDK calls their `Close` at most once. The caller must close the stream it receives from the SDK client.

In an author's `*_test.go`, use `sdktest.NewClient(t, sdk.ServerOptions{PluginID: "example.storage", Version: "1.2.3"}, driver)` on a system with Unix-domain sockets. The utility starts the same SDK server and authenticates a real SDK client. It manages the socket and both peers through test cleanup. Call the returned client to test the driver's actual wire behavior.
