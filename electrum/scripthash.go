package electrum

import (
	"context"
	"strconv"
)

// TokenFilter selects which UTXOs a token-aware call returns, via the optional token_filter
// argument of blockchain.scripthash.{listunspent,get_balance} (Electrum-Cash protocol >= 1.5.0).
type TokenFilter string

const (
	// TokenFilterInclude returns both plain-BCH and CashToken UTXOs.
	TokenFilterInclude TokenFilter = "include_tokens"
	// TokenFilterExclude returns only plain-BCH UTXOs (no CashTokens). This is the default this
	// package sends when no filter is supplied, preserving pre-1.5 BCH-only behaviour.
	TokenFilterExclude TokenFilter = "exclude_tokens"
	// TokenFilterOnly returns only CashToken UTXOs.
	TokenFilterOnly TokenFilter = "tokens_only"
)

// resolveTokenFilter picks the token_filter to send. When the caller supplies none we send
// exclude_tokens explicitly: under a negotiated 1.5.x protocol the server default is
// include_tokens, so being explicit keeps no-arg calls BCH-only (tokens are strictly opt-in).
func resolveTokenFilter(tokenFilter []TokenFilter) TokenFilter {
	if len(tokenFilter) > 0 {
		return tokenFilter[0]
	}
	return TokenFilterExclude
}

// NFTData describes the non-fungible part of a CashToken output, when present.
type NFTData struct {
	Capability string `json:"capability"` // "none" | "mutable" | "minting"
	Commitment string `json:"commitment"` // hex (may be empty)
}

// TokenData describes the CashToken carried by a UTXO (BCH May-2023 upgrade). It is present only
// for token outputs. A bare fungible token has NFT == nil and Amount > 0; a pure NFT has
// Amount == "0" and NFT != nil; both may be set together.
type TokenData struct {
	Category string   `json:"category"` // 32-byte category id, hex (display byte order)
	Amount   string   `json:"amount"`   // fungible atoms, sent as a JSON string (int64)
	NFT      *NFTData `json:"nft,omitempty"`
}

// AmountInt64 parses the fungible token amount (sent by the protocol as a decimal string) into
// an int64 number of atoms. An empty amount is treated as 0.
func (td *TokenData) AmountInt64() (int64, error) {
	if td == nil || td.Amount == "" {
		return 0, nil
	}
	return strconv.ParseInt(td.Amount, 10, 64)
}

// HasNFT reports whether this token output carries a non-fungible token.
func (td *TokenData) HasNFT() bool {
	return td != nil && td.NFT != nil
}

// GetBalanceResp represents the response to GetBalance().
type GetBalanceResp struct {
	Result GetBalanceResult `json:"result"`
}

// GetBalanceResult represents the content of the result field in the response to GetBalance().
type GetBalanceResult struct {
	Confirmed   float64 `json:"confirmed"`
	Unconfirmed float64 `json:"unconfirmed"`
}

// GetBalance returns the confirmed and unconfirmed balance for a scripthash. The optional
// tokenFilter selects whether CashToken UTXOs are counted; when omitted it defaults to
// exclude_tokens (BCH only).
// https://electrumx.readthedocs.io/en/latest/protocol-methods.html#blockchain-scripthash-get-balance
func (s *Client) GetBalance(ctx context.Context, scripthash string, tokenFilter ...TokenFilter) (GetBalanceResult, error) {
	var resp GetBalanceResp

	params := []interface{}{scripthash, string(resolveTokenFilter(tokenFilter))}
	err := s.request(ctx, "blockchain.scripthash.get_balance", params, &resp)
	if err != nil {
		return GetBalanceResult{}, err
	}

	return resp.Result, err
}

// GetMempoolResp represents the response to GetHistory() and GetMempool().
type GetMempoolResp struct {
	Result []*GetMempoolResult `json:"result"`
}

// GetMempoolResult represents the content of the result field in the response
// to GetHistory() and GetMempool().
type GetMempoolResult struct {
	Hash   string `json:"tx_hash"`
	Height int32  `json:"height"`
	Fee    uint32 `json:"fee,omitempty"`
}

// GetHistory returns the confirmed and unconfirmed history for a scripthash.
func (s *Client) GetHistory(ctx context.Context, scripthash string) ([]*GetMempoolResult, error) {
	var resp GetMempoolResp

	err := s.request(ctx, "blockchain.scripthash.get_history", []interface{}{scripthash}, &resp)
	if err != nil {
		return nil, err
	}

	return resp.Result, err
}

// GetMempool returns the unconfirmed transacations of a scripthash.
func (s *Client) GetMempool(ctx context.Context, scripthash string) ([]*GetMempoolResult, error) {
	var resp GetMempoolResp

	err := s.request(ctx, "blockchain.scripthash.get_mempool", []interface{}{scripthash}, &resp)
	if err != nil {
		return nil, err
	}

	return resp.Result, err
}

// ListUnspentResp represents the response to ListUnspent()
type ListUnspentResp struct {
	Result []*ListUnspentResult `json:"result"`
}

// ListUnspentResult represents the content of the result field in the response to ListUnspent()
type ListUnspentResult struct {
	Height    uint32     `json:"height"`
	Position  uint32     `json:"tx_pos"`
	Hash      string     `json:"tx_hash"`
	Value     uint64     `json:"value"`
	TokenData *TokenData `json:"token_data,omitempty"` // present only for CashToken UTXOs (protocol >= 1.5.0)
}

// ListUnspent returns an ordered list of UTXOs for a scripthash. The optional tokenFilter selects
// which UTXOs are returned (include/exclude/tokens_only); when omitted it defaults to
// exclude_tokens (BCH only), so callers must opt in with TokenFilterInclude/TokenFilterOnly to see
// CashToken UTXOs and their token_data.
func (s *Client) ListUnspent(ctx context.Context, scripthash string, tokenFilter ...TokenFilter) ([]*ListUnspentResult, error) {
	var resp ListUnspentResp

	params := []interface{}{scripthash, string(resolveTokenFilter(tokenFilter))}
	err := s.request(ctx, "blockchain.scripthash.listunspent", params, &resp)
	if err != nil {
		return nil, err
	}

	return resp.Result, err
}

// ListUnspentBatch returns an ordered list of UTXOs for a scripthash.
func (s *Client) ListUnspentBatch(ctx context.Context, scripthash []string, tokenFilter ...TokenFilter) ([]*ListUnspentResult, error) {
	var resp []ListUnspentResp

	// TODO must be sent as object, we need requestBatch() https://github.com/cculianu/Fulcrum/issues/99
	// TODO still not supported? res [{"id":3,"jsonrpc":"2.0","result":[]},{"id":2,"jsonrpc":"2.0","result":[]},{"id":4,"jsonrpc":"2.0","result":[]}]
	filter := string(resolveTokenFilter(tokenFilter))
	method := make([]string, len(scripthash))
	params := make([][]interface{}, len(scripthash))
	for i, v := range scripthash {
		method[i] = "blockchain.scripthash.listunspent"
		params[i] = []interface{}{v, filter}
	}
	err := s.requestBatch(ctx, method, params, &resp)
	if err != nil {
		return nil, err
	}

	//return resp.Result, err
	return nil, nil // TODO
}
