package electrum

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// ClientVersion identifies the client version/name to the remote server
	ClientVersion = "go-electrum1.1"

	// ProtocolVersion identifies the support protocol version to the remote server.
	// 1.5.x is required for BCH CashToken support (server.version must negotiate >= 1.5.0
	// before the server will return token_data / accept the token_filter argument).
	ProtocolVersion = "1.5.3"

	// defaultHandshakeTimeout bounds how long the automatic server.version negotiation
	// performed on connect may take, even if the caller passes a context without a deadline.
	defaultHandshakeTimeout = 15 * time.Second

	nl = byte('\n')
)

var (
	// DebugMode provides debug output on communications with the remote server if enabled.
	DebugMode bool

	// ErrServerConnected throws an error if remote server is already connected.
	ErrServerConnected = errors.New("server is already connected")

	// ErrServerShutdown throws an error if remote server has shutdown.
	ErrServerShutdown = errors.New("server has shutdown")

	// ErrTimeout throws an error if request has timed out
	ErrTimeout = errors.New("request timeout")

	// ErrNotImplemented throws an error if this RPC call has not been implemented yet.
	ErrNotImplemented = errors.New("RPC call is not implemented")

	// ErrDeprecated throws an error if this RPC call is deprecated.
	ErrDeprecated = errors.New("RPC call has been deprecated")
)

// Transport provides interface to server transport.
type Transport interface {
	SendMessage([]byte) error
	Responses() <-chan []byte
	Errors() <-chan error
	Close() error
}

type container struct {
	content []byte
	err     error
}

// Client stores information about the remote server.
type Client struct {
	transport   Transport
	transportMu sync.Mutex //Ekliptor> Transport mutex

	handlers     map[uint64]chan *container
	handlersLock sync.RWMutex

	pushHandlers     map[string][]chan *container
	pushHandlersLock sync.RWMutex

	Error chan error
	quit  chan struct{}
	once  sync.Once //Ekliptor> fix channel close panic

	nextID uint64

	// serverVer / protocolVer hold the result of the server.version negotiation performed on
	// connect. They are written once (before the constructor returns) and read-only afterwards.
	serverVer   string
	protocolVer string
}

// negotiateProtocol sends server.version to identify the client and negotiate the protocol
// version. This must happen before any other request, or the server defaults to an older
// protocol (and, on BCH, will not return token_data / accept the token_filter argument).
func (s *Client) negotiateProtocol(ctx context.Context) error {
	hctx, cancel := context.WithTimeout(ctx, defaultHandshakeTimeout)
	defer cancel()

	serverVer, protocolVer, err := s.ServerVersion(hctx)
	if err != nil {
		return err
	}
	s.serverVer = serverVer
	s.protocolVer = protocolVer
	return nil
}

// NegotiatedProtocolVersion returns the protocol version negotiated with the server during
// connect (e.g. "1.5.3"), or "" if negotiation has not completed.
func (s *Client) NegotiatedProtocolVersion() string {
	return s.protocolVer
}

// ServerSoftwareVersion returns the server software identifier reported during the connect
// negotiation (e.g. "Fulcrum 2.0"), or "" if negotiation has not completed.
func (s *Client) ServerSoftwareVersion() string {
	return s.serverVer
}

// NewClientTCP initialize a new client for remote server and connects to the remote server using TCP
func NewClientTCP(ctx context.Context, addr string) (*Client, error) {
	transport, err := NewTCPTransport(ctx, addr)
	if err != nil {
		return nil, err
	}

	c := &Client{
		handlers:     make(map[uint64]chan *container),
		pushHandlers: make(map[string][]chan *container),

		Error: make(chan error),
		quit:  make(chan struct{}),
	}

	c.transport = transport
	go c.listen()

	// Negotiate the protocol version immediately, so token-aware calls work and every
	// reconnect (which recreates the client via this constructor) renegotiates.
	if err := c.negotiateProtocol(ctx); err != nil {
		c.Shutdown()
		return nil, err
	}

	return c, nil
}

// NewClientSSL initialize a new client for remote server and connects to the remote server using SSL
func NewClientSSL(ctx context.Context, addr string, config *tls.Config) (*Client, error) {
	transport, err := NewSSLTransport(ctx, addr, config)
	if err != nil {
		return nil, err
	}

	c := &Client{
		handlers:     make(map[uint64]chan *container),
		pushHandlers: make(map[string][]chan *container),

		Error: make(chan error),
		quit:  make(chan struct{}),
	}

	c.transport = transport
	go c.listen()

	// Negotiate the protocol version immediately, so token-aware calls work and every
	// reconnect (which recreates the client via this constructor) renegotiates.
	if err := c.negotiateProtocol(ctx); err != nil {
		c.Shutdown()
		return nil, err
	}

	return c, nil
}

type apiErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *apiErr) Error() string {
	return fmt.Sprintf("errNo: %d, errMsg: %s", e.Code, e.Message)
}

type response struct {
	ID     uint64 `json:"id"`
	Method string `json:"method"`
	Error  string `json:"error"`
}

func (s *Client) listen() {
	for {
		if s.IsShutdown() {
			break
		}
		//Ekliptor> Transport mutex: capture the transport under the lock and use the local below,
		// so the select does not read s.transport concurrently with Shutdown() setting it to nil
		// (data race + nil-deref crash).
		s.transportMu.Lock()
		transport := s.transport
		s.transportMu.Unlock()
		if transport == nil {
			break
		}
		select {
		case <-s.quit:
			return
		case err := <-transport.Errors():
			//Ekliptor> nobody may be reading s.Error: don't block forever (goroutine + socket leak),
			// shut down so pending and future requests fail fast with ErrServerShutdown
			select {
			case s.Error <- err:
			default:
			}
			s.Shutdown()
			return
			//Ekliptor< nobody may be reading s.Error
		case bytes := <-transport.Responses():
			result := &container{
				content: bytes,
			}

			msg := &response{}
			err := json.Unmarshal(bytes, msg)
			if err != nil {
				if DebugMode {
					log.Printf("Unmarshal received message failed: %v", err)
				}
				result.err = fmt.Errorf("unmarshal received message failed: %v - response: %s", err, string(bytes))
			} else if msg.Error != "" {
				result.err = errors.New(msg.Error)
			}

			if len(msg.Method) > 0 {
				s.pushHandlersLock.RLock()
				handlers := s.pushHandlers[msg.Method]
				s.pushHandlersLock.RUnlock()

				for _, handler := range handlers {
					select {
					case handler <- result:
					default:
					}
				}
			}

			s.handlersLock.RLock()
			c, ok := s.handlers[msg.ID]
			s.handlersLock.RUnlock()

			if ok {
				// TODO: very rare case. fix this memory leak, when nobody will read channel (in case of error)
				c <- result
			}
		}
	}
}

func (s *Client) listenPush(method string) <-chan *container {
	c := make(chan *container, 1)
	s.pushHandlersLock.Lock()
	s.pushHandlers[method] = append(s.pushHandlers[method], c)
	s.pushHandlersLock.Unlock()

	return c
}

type request struct {
	ID     uint64        `json:"id"`
	Method string        `json:"method"`
	Params []interface{} `json:"params"`
}

func (s *Client) request(ctx context.Context, method string, params []interface{}, v interface{}) error {
	select {
	case <-s.quit:
		return ErrServerShutdown
	default:
	}

	msg := request{
		ID:     atomic.AddUint64(&s.nextID, 1),
		Method: method,
		Params: params,
	}

	bytes, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	bytes = append(bytes, nl)

	//Ekliptor> Transport mutex
	s.transportMu.Lock()
	if s.transport != nil {
		err = s.transport.SendMessage(bytes)
	} else {
		err = ErrServerShutdown
	}
	s.transportMu.Unlock()
	//Ekliptor< Transport mutex
	if err != nil {
		s.Shutdown()
		return err
	}

	c := make(chan *container, 1)

	s.handlersLock.Lock()
	//Ekliptor> fix nil map cash on shutdown (reconnect)
	if s.IsShutdown() || s.handlers == nil {
		s.handlersLock.Unlock()
		return ErrServerShutdown
	}
	//Ekliptor< fix nil map cash on shutdown (reconnect)
	s.handlers[msg.ID] = c
	s.handlersLock.Unlock()

	defer func() {
		s.handlersLock.Lock()
		delete(s.handlers, msg.ID)
		s.handlersLock.Unlock()
	}()

	var resp *container
	select {
	case resp = <-c:
	case <-ctx.Done():
		return ErrTimeout
	}

	if resp.err != nil {
		return resp.err
	}

	if v != nil {
		err = json.Unmarshal(resp.content, v)
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *Client) requestBatch(ctx context.Context, method []string, params [][]interface{}, v interface{}) error {
	select {
	case <-s.quit:
		return ErrServerShutdown
	default:
	}

	if len(method) == 0 {
		return errors.New("method can not be empty")
	} else if len(method) != len(params) {
		return errors.New("method and params must have the same length")
	}

	msg := make([]request, len(method))
	for i := range method {
		msg[i] = request{
			ID:     atomic.AddUint64(&s.nextID, 1),
			Method: method[i],
			Params: params[i],
		}
	}

	bytes, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	bytes = append(bytes, nl)

	//Ekliptor> Transport mutex
	s.transportMu.Lock()
	if s.transport != nil {
		err = s.transport.SendMessage(bytes)
	} else {
		err = ErrServerShutdown
	}
	s.transportMu.Unlock()
	//Ekliptor< Transport mutex
	if err != nil {
		s.Shutdown()
		return err
	}

	c := make(chan *container, 1)

	s.handlersLock.Lock()
	//Ekliptor> fix nil map cash on shutdown (reconnect)
	if s.IsShutdown() || s.handlers == nil {
		s.handlersLock.Unlock()
		return ErrServerShutdown
	}
	//Ekliptor< fix nil map cash on shutdown (reconnect)
	s.handlers[msg[0].ID] = c // use the 1st msg to receive the stream and for mutex
	s.handlersLock.Unlock()

	defer func() {
		s.handlersLock.Lock()
		delete(s.handlers, msg[0].ID)
		s.handlersLock.Unlock()
	}()

	var resp *container
	select {
	case resp = <-c:
	case <-ctx.Done():
		return ErrTimeout
	}

	if resp.err != nil {
		return resp.err
	}

	if v != nil {
		err = json.Unmarshal(resp.content, v)
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *Client) Shutdown() {
	if !s.IsShutdown() {
		//Ekliptor> fix channel close panic
		//val, ok := <-s.quit; if ok {
		s.once.Do(func() { close(s.quit) })
		//Ekliptor< fix channel close panic
	}
	//Ekliptor> Transport mutex
	s.transportMu.Lock()
	if s.transport != nil {
		_ = s.transport.Close()
	}
	s.transport = nil
	s.transportMu.Unlock()
	//Ekliptor< Transport mutex
	//Ekliptor> lock handler maps: listen() may shut down while a request still deletes its handler
	s.handlersLock.Lock()
	s.handlers = nil
	s.handlersLock.Unlock()
	s.pushHandlersLock.Lock()
	s.pushHandlers = nil
	s.pushHandlersLock.Unlock()
	//Ekliptor< lock handler maps
}

func (s *Client) IsShutdown() bool {
	select {
	case <-s.quit:
		return true
	default:
	}
	return false
}
