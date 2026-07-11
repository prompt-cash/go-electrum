package electrum

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sampleCategory is the token category used in the Electrum-Cash protocol documentation example.
const sampleCategory = "8fd6a2f713beaa5907a776b8b3060cddd1c6ff0588554c2364698ae271321ce9"

// listUnspentJSON is a realistic blockchain.scripthash.listunspent response (include_tokens),
// mixing a plain-BCH UTXO, a bare fungible CashToken UTXO, and a pure-NFT CashToken UTXO.
const listUnspentJSON = `{
  "id": 1,
  "jsonrpc": "2.0",
  "result": [
    {
      "height": 800000,
      "tx_pos": 0,
      "tx_hash": "1111111111111111111111111111111111111111111111111111111111111111",
      "value": 250000
    },
    {
      "height": 800001,
      "tx_pos": 1,
      "tx_hash": "2222222222222222222222222222222222222222222222222222222222222222",
      "value": 1000,
      "token_data": {
        "amount": "1000000",
        "category": "8fd6a2f713beaa5907a776b8b3060cddd1c6ff0588554c2364698ae271321ce9"
      }
    },
    {
      "height": 800002,
      "tx_pos": 0,
      "tx_hash": "3333333333333333333333333333333333333333333333333333333333333333",
      "value": 546,
      "token_data": {
        "amount": "0",
        "category": "8fd6a2f713beaa5907a776b8b3060cddd1c6ff0588554c2364698ae271321ce9",
        "nft": {
          "capability": "minting",
          "commitment": "f00fd00fb33f"
        }
      }
    }
  ]
}`

func TestListUnspentResultTokenDataParsing(t *testing.T) {
	var resp ListUnspentResp
	require.NoError(t, json.Unmarshal([]byte(listUnspentJSON), &resp))
	require.Len(t, resp.Result, 3)

	// (a) plain BCH UTXO: no token_data.
	bch := resp.Result[0]
	assert.Equal(t, uint64(250000), bch.Value)
	assert.Nil(t, bch.TokenData, "plain BCH UTXO must have nil TokenData")
	assert.False(t, bch.TokenData.HasNFT())

	// (b) bare fungible token UTXO: amount > 0, no NFT.
	ft := resp.Result[1]
	require.NotNil(t, ft.TokenData)
	assert.Equal(t, sampleCategory, ft.TokenData.Category)
	assert.Equal(t, "1000000", ft.TokenData.Amount)
	assert.Nil(t, ft.TokenData.NFT)
	assert.False(t, ft.TokenData.HasNFT())
	amt, err := ft.TokenData.AmountInt64()
	require.NoError(t, err)
	assert.Equal(t, int64(1000000), amt)

	// (c) pure NFT UTXO: amount == 0, NFT present. This is the form the PUSD detection
	// predicate must EXCLUDE (it is not the fungible stablecoin).
	nft := resp.Result[2]
	require.NotNil(t, nft.TokenData)
	assert.Equal(t, sampleCategory, nft.TokenData.Category)
	assert.True(t, nft.TokenData.HasNFT())
	require.NotNil(t, nft.TokenData.NFT)
	assert.Equal(t, "minting", nft.TokenData.NFT.Capability)
	assert.Equal(t, "f00fd00fb33f", nft.TokenData.NFT.Commitment)
	amt, err = nft.TokenData.AmountInt64()
	require.NoError(t, err)
	assert.Equal(t, int64(0), amt)
}

func TestTokenDataAmountInt64(t *testing.T) {
	tests := []struct {
		name    string
		td      *TokenData
		want    int64
		wantErr bool
	}{
		{name: "nil token data", td: nil, want: 0},
		{name: "empty string", td: &TokenData{Amount: ""}, want: 0},
		{name: "zero", td: &TokenData{Amount: "0"}, want: 0},
		{name: "small", td: &TokenData{Amount: "10000"}, want: 10000},
		{name: "max int64", td: &TokenData{Amount: "9223372036854775807"}, want: 9223372036854775807},
		{name: "non-numeric", td: &TokenData{Amount: "abc"}, wantErr: true},
		{name: "overflow", td: &TokenData{Amount: "9223372036854775808"}, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.td.AmountInt64()
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestResolveTokenFilter(t *testing.T) {
	// No filter supplied => exclude_tokens (BCH only), so existing no-arg call sites keep
	// their pre-1.5 behaviour even though the server's 1.5 default is include_tokens.
	assert.Equal(t, TokenFilterExclude, resolveTokenFilter(nil))
	assert.Equal(t, TokenFilterExclude, resolveTokenFilter([]TokenFilter{}))

	// Explicit filters pass through; only the first is honoured.
	assert.Equal(t, TokenFilterInclude, resolveTokenFilter([]TokenFilter{TokenFilterInclude}))
	assert.Equal(t, TokenFilterOnly, resolveTokenFilter([]TokenFilter{TokenFilterOnly}))
	assert.Equal(t, TokenFilterExclude, resolveTokenFilter([]TokenFilter{TokenFilterExclude}))
	assert.Equal(t, TokenFilterInclude, resolveTokenFilter([]TokenFilter{TokenFilterInclude, TokenFilterOnly}))

	// Wire values must match the protocol spec exactly.
	assert.Equal(t, "include_tokens", string(TokenFilterInclude))
	assert.Equal(t, "exclude_tokens", string(TokenFilterExclude))
	assert.Equal(t, "tokens_only", string(TokenFilterOnly))
}
