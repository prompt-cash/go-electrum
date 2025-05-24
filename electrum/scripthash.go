package electrum

import "context"

// GetBalanceResp represents the response to GetBalance().
type GetBalanceResp struct {
	Result GetBalanceResult `json:"result"`
}

// GetBalanceResult represents the content of the result field in the response to GetBalance().
type GetBalanceResult struct {
	Confirmed   float64 `json:"confirmed"`
	Unconfirmed float64 `json:"unconfirmed"`
}

// GetBalance returns the confirmed and unconfirmed balance for a scripthash.
// https://electrumx.readthedocs.io/en/latest/protocol-methods.html#blockchain-scripthash-get-balance
func (s *Client) GetBalance(ctx context.Context, scripthash string) (GetBalanceResult, error) {
	var resp GetBalanceResp

	err := s.request(ctx, "blockchain.scripthash.get_balance", []interface{}{scripthash}, &resp)
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
	Height   uint32 `json:"height"`
	Position uint32 `json:"tx_pos"`
	Hash     string `json:"tx_hash"`
	Value    uint64 `json:"value"`
}

// ListUnspent returns an ordered list of UTXOs for a scripthash.
func (s *Client) ListUnspent(ctx context.Context, scripthash string) ([]*ListUnspentResult, error) {
	var resp ListUnspentResp

	err := s.request(ctx, "blockchain.scripthash.listunspent", []interface{}{scripthash}, &resp)
	if err != nil {
		return nil, err
	}

	return resp.Result, err
}

// ListUnspentBatch returns an ordered list of UTXOs for a scripthash.
func (s *Client) ListUnspentBatch(ctx context.Context, scripthash []string) ([]*ListUnspentResult, error) {
	var resp []ListUnspentResp

	// TODO must be sent as object, we need requestBatch() https://github.com/cculianu/Fulcrum/issues/99
	// TODO still not supported? res [{"id":3,"jsonrpc":"2.0","result":[]},{"id":2,"jsonrpc":"2.0","result":[]},{"id":4,"jsonrpc":"2.0","result":[]}]
	method := make([]string, len(scripthash))
	params := make([][]interface{}, len(scripthash))
	for i, v := range scripthash {
		method[i] = "blockchain.scripthash.listunspent"
		params[i] = []interface{}{v}
	}
	err := s.requestBatch(ctx, method, params, &resp)
	if err != nil {
		return nil, err
	}

	//return resp.Result, err
	return nil, nil // TODO
}
