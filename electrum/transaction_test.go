package electrum

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pusdCategory is the token category of ParyonUSD (PUSD) on BCH mainnet.
const pusdCategory = "2469acc5afa4b10cb5b5c04afb89c3a3ffd61c5da9c01e26d00951cae2a02544"

// verboseTxJSON is a blockchain.transaction.get (verbose) response for mainnet tx
// cd999409aad5dcd8a2b9a39e147a046a8aee2f5bf130994cb67f937cf065fac3, as returned by Fulcrum 2.1.4
// after negotiating protocol 1.5.3. It is trimmed to three outputs, copied verbatim: a PUSD
// minting NFT that also carries fungible supply (n=0), a plain BCH output (n=4), and a bare
// fungible PUSD payment of 20200 atoms (n=6).
const verboseTxJSON = `{
  "id": 1,
  "jsonrpc": "2.0",
  "result": {
    "confirmations": 29436,
    "hash": "cd999409aad5dcd8a2b9a39e147a046a8aee2f5bf130994cb67f937cf065fac3",
    "vout": [
      {"n":0,"scriptPubKey":{"addresses":["bitcoincash:pvszuucxz7fn4pthapvfk3xy8gxjhpvcm9yjt2przz44v5z2ax7v6alt8qj02"],"asm":"OP_HASH256 202e730617933a8577e8589b44c43a0d2b8598d94925a82310ab56504ae9bccd OP_EQUAL","hex":"aa20202e730617933a8577e8589b44c43a0d2b8598d94925a82310ab56504ae9bccd87","reqSigs":1,"type":"scripthash"},"tokenData":{"amount":"922337203685457380","category":"2469acc5afa4b10cb5b5c04afb89c3a3ffd61c5da9c01e26d00951cae2a02544","nft":{"capability":"minting","commitment":"00000000"}},"value":1e-05},
      {"n":4,"scriptPubKey":{"addresses":["bitcoincash:pd785fyv09909n03kxnx5gc3x37ytvthlje644es57pre83jl365qkw9zfvw6"],"asm":"OP_HASH256 7c7a248c794af2cdf1b1a66a2311347c45b177fcb3aad730a7823c9e32fc7540 OP_EQUAL","hex":"aa207c7a248c794af2cdf1b1a66a2311347c45b177fcb3aad730a7823c9e32fc754087","reqSigs":1,"type":"scripthash"},"value":0.00112003},
      {"n":6,"scriptPubKey":{"addresses":["bitcoincash:qz8wymtvnavrd8u5sexuxccvm6chlt3095hczr7px4"],"asm":"OP_DUP OP_HASH160 8ee26d6c9f58369f94864dc3630cdeb17fae2f2d OP_EQUALVERIFY OP_CHECKSIG","hex":"76a9148ee26d6c9f58369f94864dc3630cdeb17fae2f2d88ac","reqSigs":1,"type":"pubkeyhash"},"tokenData":{"amount":"20200","category":"2469acc5afa4b10cb5b5c04afb89c3a3ffd61c5da9c01e26d00951cae2a02544"},"value":1e-05}
    ]
  }
}`

func TestGetTransactionResultTokenDataParsing(t *testing.T) {
	var resp GetTransactionResp
	require.NoError(t, json.Unmarshal([]byte(verboseTxJSON), &resp))
	require.NotNil(t, resp.Result)
	require.Len(t, resp.Result.Vout, 3)

	// (a) minting NFT that also carries fungible supply.
	mint := resp.Result.Vout[0]
	require.NotNil(t, mint.TokenData, "tokenData must be decoded from the verbose tx")
	assert.Equal(t, pusdCategory, mint.TokenData.Category)
	assert.True(t, mint.TokenData.HasNFT())
	assert.Equal(t, "minting", mint.TokenData.NFT.Capability)
	assert.Equal(t, "00000000", mint.TokenData.NFT.Commitment)
	amt, err := mint.TokenData.AmountInt64()
	require.NoError(t, err)
	assert.Equal(t, int64(922337203685457380), amt)

	// (b) plain BCH output: no tokenData.
	bch := resp.Result.Vout[1]
	assert.Equal(t, uint32(4), bch.N)
	assert.Nil(t, bch.TokenData)
	assert.InDelta(t, 0.00112003, bch.Value, 1e-12)

	// (c) bare fungible PUSD payment: what a merchant matches an invoice against.
	pay := resp.Result.Vout[2]
	assert.Equal(t, uint32(6), pay.N)
	assert.Equal(t, []string{"bitcoincash:qz8wymtvnavrd8u5sexuxccvm6chlt3095hczr7px4"}, pay.ScriptPubkey.Addresses)
	require.NotNil(t, pay.TokenData, "tokenData must be decoded from the verbose tx")
	assert.Equal(t, pusdCategory, pay.TokenData.Category)
	assert.False(t, pay.TokenData.HasNFT())
	amt, err = pay.TokenData.AmountInt64()
	require.NoError(t, err)
	assert.Equal(t, int64(20200), amt)
}
