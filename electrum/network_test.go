package electrum

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"
)

// TestListenTransportError checks that the client shuts itself down when the server drops the connection,
// even though nobody reads Client.Error.
func TestListenTransportError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close() // drop the connection right after the handshake

		line, err := bufio.NewReader(conn).ReadBytes(nl)
		if err != nil {
			return
		}
		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			return
		}
		time.Sleep(50 * time.Millisecond) // request() registers its handler after sending
		fmt.Fprintf(conn, `{"jsonrpc":"2.0","id":%d,"result":["test-server","%s"]}`+"\n", req.ID, ProtocolVersion)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := NewClientTCP(ctx, ln.Addr().String())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for !client.IsShutdown() {
		if time.Now().After(deadline) {
			t.Fatal("client is not shut down 1s after the server closed the connection")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if _, err := client.ListUnspent(ctx, "8b01df4e368ea28f8dc0423bcf7a4923e3a12d307c875e47a0cfbf90b5c39161"); !errors.Is(err, ErrServerShutdown) {
		t.Fatalf("expected ErrServerShutdown after the connection dropped, got: %v", err)
	}
}
