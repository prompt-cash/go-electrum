package electrum

import (
	"context"
	"encoding/hex"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests exercise a live Fulcrum server. They are skipped in -short mode and when the
// server cannot be reached, so `go test ./...` stays green without network access.
//
//	Server:     ELECTRUM_TEST_SERVER      (default 127.0.0.1:60001)
//	Token UTXO: ELECTRUM_TEST_TOKEN_SCRIPTHASH (optional; a scripthash known to hold CashTokens)
func testServerAddr() string {
	if v := os.Getenv("ELECTRUM_TEST_SERVER"); v != "" {
		return v
	}
	return "127.0.0.1:60001"
}

// dialTestClient connects to the live server, skipping the test if it is unreachable.
func dialTestClient(t *testing.T) *Client {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping live Fulcrum integration test in -short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := NewClientTCP(ctx, testServerAddr())
	if err != nil {
		t.Skipf("live Fulcrum at %s unreachable (%v); skipping", testServerAddr(), err)
	}
	t.Cleanup(client.Shutdown)
	return client
}

// protocolAtLeast reports whether the dotted protocol version is >= major.minor.
func protocolAtLeast(version string, major, minor int) bool {
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return false
	}
	gotMajor, err1 := strconv.Atoi(parts[0])
	gotMinor, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return false
	}
	if gotMajor != major {
		return gotMajor > major
	}
	return gotMinor >= minor
}

func TestProtocolAtLeast(t *testing.T) {
	assert.True(t, protocolAtLeast("1.5.3", 1, 5))
	assert.True(t, protocolAtLeast("1.5.0", 1, 5))
	assert.True(t, protocolAtLeast("2.0", 1, 5))
	assert.False(t, protocolAtLeast("1.4", 1, 5))
	assert.False(t, protocolAtLeast("1.4.2", 1, 5))
	assert.False(t, protocolAtLeast("garbage", 1, 5))
}

// TestIntegrationNegotiatesCashTokenProtocol verifies that connecting negotiates a
// CashToken-capable protocol (>= 1.5.0) and that the token_filter argument is accepted.
func TestIntegrationNegotiatesCashTokenProtocol(t *testing.T) {
	client := dialTestClient(t)

	proto := client.NegotiatedProtocolVersion()
	t.Logf("connected to %q, negotiated protocol %q", client.ServerSoftwareVersion(), proto)
	require.NotEmpty(t, proto, "protocol must be negotiated on connect")
	assert.True(t, protocolAtLeast(proto, 1, 5),
		"negotiated protocol %q must be >= 1.5.0 for CashToken support", proto)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// A well-formed but (almost certainly) empty scripthash. We only assert the server accepts
	// each token_filter without a protocol error.
	scripthash := strings.Repeat("a", 64)
	for _, f := range []TokenFilter{TokenFilterExclude, TokenFilterInclude, TokenFilterOnly} {
		_, err := client.ListUnspent(ctx, scripthash, f)
		assert.NoErrorf(t, err, "listunspent with token_filter=%s should be accepted", f)
	}
	_, err := client.GetBalance(ctx, scripthash, TokenFilterInclude)
	assert.NoError(t, err, "get_balance with token_filter should be accepted")
}

// TestIntegrationTokenDataOnRealUTXO parses real token_data from a known token-holding
// scripthash. It only runs when ELECTRUM_TEST_TOKEN_SCRIPTHASH is set.
func TestIntegrationTokenDataOnRealUTXO(t *testing.T) {
	scripthash := os.Getenv("ELECTRUM_TEST_TOKEN_SCRIPTHASH")
	if scripthash == "" {
		t.Skip("set ELECTRUM_TEST_TOKEN_SCRIPTHASH to a CashToken-holding scripthash to run this test")
	}
	client := dialTestClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	utxos, err := client.ListUnspent(ctx, scripthash, TokenFilterOnly)
	require.NoError(t, err)
	require.NotEmpty(t, utxos, "tokens_only listunspent returned no UTXOs for %s", scripthash)

	tokenCount := 0
	for _, u := range utxos {
		require.NotNil(t, u.TokenData, "tokens_only UTXO must carry token_data")
		td := u.TokenData

		raw, err := hex.DecodeString(td.Category)
		require.NoError(t, err, "category must be hex")
		assert.Len(t, raw, 32, "category must be a 32-byte id")

		amt, err := td.AmountInt64()
		require.NoError(t, err, "amount must parse as int64")
		assert.GreaterOrEqual(t, amt, int64(0))

		t.Logf("utxo %s:%d value=%d category=%s amount=%d nft=%v",
			u.Hash, u.Position, u.Value, td.Category, amt, td.HasNFT())
		tokenCount++
	}
	assert.Positive(t, tokenCount)
}
