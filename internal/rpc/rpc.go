// Package rpc is a tiny JSON-RPC 2.0 implementation over newline-delimited JSON.
//
// Ported from scry — identical wire format. See scry/internal/rpc/rpc.go for
// extended commentary.
//
// Wire format: each request and response is a single JSON object terminated
// by a single '\n'. Both directions speak the same envelope shape.
//
//	Request:  {"jsonrpc":"2.0","id":1,"method":"describe","params":{...}}
//	Response: {"jsonrpc":"2.0","id":1,"result":{...}}
//	Error:    {"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"..."}}
package rpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// JSON-RPC 2.0 standard error codes.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
)

// Request is the JSON-RPC 2.0 envelope received by the server.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is the envelope sent back to the client.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// Error is the JSON-RPC 2.0 error object.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message)
}

// HandlerFunc handles one JSON-RPC method.
type HandlerFunc func(ctx context.Context, params json.RawMessage) (any, error)

// Server is a JSON-RPC 2.0 server bound to a net.Listener.
type Server struct {
	mu      sync.RWMutex
	methods map[string]HandlerFunc
}

func NewServer() *Server {
	return &Server{methods: map[string]HandlerFunc{}}
}

func (s *Server) Register(method string, h HandlerFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.methods[method] = h
}

// Serve accepts connections and dispatches requests until ctx is done.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) || ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("accept: %w", err)
		}
		go s.serveConn(ctx, conn)
	}
}

func (s *Server) serveConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	enc := json.NewEncoder(conn)
	for scanner.Scan() {
		var req Request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			_ = enc.Encode(Response{
				JSONRPC: "2.0",
				Error:   &Error{Code: CodeParseError, Message: err.Error()},
			})
			continue
		}
		s.mu.RLock()
		h, ok := s.methods[req.Method]
		s.mu.RUnlock()
		if !ok {
			_ = enc.Encode(Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &Error{Code: CodeMethodNotFound, Message: "method not found: " + req.Method},
			})
			continue
		}
		result, err := h(ctx, req.Params)
		resp := Response{JSONRPC: "2.0", ID: req.ID}
		if err != nil {
			var rpcErr *Error
			if errors.As(err, &rpcErr) {
				resp.Error = rpcErr
			} else {
				resp.Error = &Error{Code: CodeInternalError, Message: err.Error()}
			}
		} else {
			b, mErr := json.Marshal(result)
			if mErr != nil {
				resp.Error = &Error{Code: CodeInternalError, Message: "marshal result: " + mErr.Error()}
			} else {
				resp.Result = b
			}
		}
		if err := enc.Encode(resp); err != nil {
			return
		}
	}
}

// Client is a JSON-RPC 2.0 client over a single net.Conn.
type Client struct {
	conn   net.Conn
	enc    *json.Encoder
	dec    *json.Decoder
	nextID atomic.Int64
}

// Dial connects to a Unix domain socket.
func Dial(socketPath string) (*Client, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("dial unix %q: %w", socketPath, err)
	}
	return &Client{
		conn: conn,
		enc:  json.NewEncoder(conn),
		dec:  json.NewDecoder(conn),
	}, nil
}

func (c *Client) Close() error {
	return c.conn.Close()
}

// Call sends a request and decodes the response into out.
func (c *Client) Call(ctx context.Context, method string, params, out any) error {
	if d, ok := ctx.Deadline(); ok {
		if err := c.conn.SetDeadline(d); err != nil {
			return err
		}
		defer c.conn.SetDeadline(time.Time{})
	}
	id := c.nextID.Add(1)
	idJSON, _ := json.Marshal(id)
	req := Request{
		JSONRPC: "2.0",
		ID:      idJSON,
		Method:  method,
	}
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("marshal params: %w", err)
		}
		req.Params = b
	}
	if err := c.enc.Encode(req); err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	var resp Response
	if err := c.dec.Decode(&resp); err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("daemon closed connection")
		}
		return fmt.Errorf("decode response: %w", err)
	}
	if resp.Error != nil {
		return resp.Error
	}
	if out != nil && len(resp.Result) > 0 {
		if err := json.Unmarshal(resp.Result, out); err != nil {
			return fmt.Errorf("decode result: %w", err)
		}
	}
	return nil
}
