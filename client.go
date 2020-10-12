package gee_rpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/futhergo/gee-rpc/codec"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// 代表依次调用
type Call struct {
	Seq           uint64
	ServiceMethod string
	Args          interface{}
	Reply         interface{}
	Error         error
	Done          chan *Call
}

func (c *Call) done() {
	c.Done <- c
}

// 代表依次连接
type Client struct {
	cc  codec.Codec // 编解码器
	opt *Option

	reqMu sync.Mutex // 保护互斥发送call
	h     codec.Header

	mu           sync.Mutex // 保护client calls更新
	seq          uint64
	pendingCalls map[uint64]*Call
	closing      bool
	shutdown     bool
}

var ErrShutdown = errors.New("connection is shut down")

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closing {
		return ErrShutdown
	}
	c.closing = true
	return c.cc.Close()
}

func (c *Client) IsAvailable() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.closing && !c.shutdown
}

func XDial(addr string, opts ...*Option) (c *Client, err error) {
	parts := strings.Split(addr, "@")
	if len(parts) != 2 {
		return nil, errors.New("unavailable addr")
	}
	protocol, addr := parts[0], parts[1]
	switch protocol {
	case "http":
		return DialHTTP("tcp", addr, opts...)
	default:
		return Dial(protocol, addr, opts...)
	}
}

// 创建Client
func Dial(network, addr string, opts ...*Option) (c *Client, err error) {
	return dial(NewClient, network, addr, opts...)
}

func DialHTTP(network, addr string, opts ...*Option) (c *Client, err error) {
	return dial(NewHTTPClient, network, addr, opts...)
}

func dial(f func(conn net.Conn, opt *Option) (*Client, error), network, addr string, opts ...*Option) (*Client, error) {
	//建立连接
	opt, _ := parseOptions(opts...)
	conn, err := net.DialTimeout(network, addr, opt.ConnTimeOut)
	if err != nil {
		return nil, err
	}
	cchan := make(chan newClientResult)
	go func() {
		c, err := f(conn, opt)
		cchan <- newClientResult{c: c, err: err}
	}()
	if opt.ConnTimeOut == 0 {
		r := <-cchan
		return r.c, r.err
	}
	select {
	case <-time.After(opt.ConnTimeOut):
		return nil, errors.New(fmt.Sprintf("connect %v create client timeout", conn))
	case r := <-cchan:
		return r.c, r.err
	}
}

type newClientResult struct {
	c   *Client
	err error
}

func parseOptions(opts ...*Option) (*Option, error) {
	if len(opts) == 0 || opts[0] == nil {
		return DefaultOption, nil
	}
	if len(opts) != 1 {
		return DefaultOption, nil
	}
	opt := opts[0]
	opt.MagicNumber = MagicNumber
	if opt.CodecType == "" {
		opt.CodecType = codec.GobType
	}
	return opt, nil
}

func NewClient(conn net.Conn, opt *Option) (c *Client, err error) {
	// 交换option协议
	// 根据option创建编解码器，创建Client，开始尝试接受
	f := codec.NewCodecFuncMap[opt.CodecType]
	if f == nil {
		err = fmt.Errorf("unknown codec type %v", opt.CodecType)
		return
	}

	if err = json.NewEncoder(conn).Encode(opt); err != nil { // 发送option，json编码
		log.Printf("send option failed %v", err)
		_ = conn.Close()
		return
	}
	c = &Client{
		cc:           f(conn), // 创建编解码器
		opt:          opt,
		seq:          1,
		pendingCalls: make(map[uint64]*Call),
	}
	go c.receive()
	return
}

func NewHTTPClient(conn net.Conn, opt *Option) (*Client, error) {
	// 发送http CONNECT
	// 收到200 connected
	_, _ = io.WriteString(conn, fmt.Sprintf("CONNECT %s HTTP/1.0\n\n", defaultRPCPath))
	resp, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: "CONNECT"})
	if err == nil && resp.Status == connected {
		return NewClient(conn, opt)
	}
	if err == nil {
		err = errors.New("unexpect HTTP CONNECT Response")
	}
	return nil, err
}

// 发送和接受call
func (c *Client) receive() {
	var err error
	for err == nil {
		var h codec.Header
		if err = c.cc.ReadHeader(&h); err != nil {
			break
		}
		call := c.removeCall(h.Seq)
		switch {
		case call == nil:
			err = c.cc.ReadBody(nil)
			if err != nil {
				err = errors.New("reading error body: " + err.Error())
			}
		case h.Err != "":
			call.Error = errors.New(h.Err)
			err = c.cc.ReadBody(nil)
			if err != nil {
				err = errors.New("reading error body: " + err.Error())
			}
		default:
			err = c.cc.ReadBody(call.Reply)
			if err != nil {
				call.Error = errors.New("reading error body: " + err.Error())
			}
			call.done()
		}
	}
	c.terminateCalls(err)
}

// 调用function，reply必须是指针类型，用于接收返回值
func (c *Client) CallMethod(ctx context.Context, serviceName string, args, reply interface{}) error {
	// 创建call
	// 发送call
	// 等待call的channel返回结果
	call := c.Go(serviceName, args, reply, make(chan *Call, 1))
	select {
	case <-ctx.Done():
		c.removeCall(call.Seq)
		return errors.New("rpc call timeout")
	case call = <-call.Done:
		return call.Error
	}
}

func (c *Client) Go(serviceName string, args, reply interface{}, done chan *Call) *Call {
	if done == nil || cap(done) == 0 {
		done = make(chan *Call, 10)
	}
	call := &Call{
		ServiceMethod: serviceName,
		Args:          args,
		Reply:         reply,
		Done:          done,
	}
	c.send(call)
	return call
}

func (c *Client) send(call *Call) {
	c.reqMu.Lock()
	defer c.reqMu.Unlock()

	// 注册call
	seq, err := c.registerCall(call)
	if err != nil {
		call.Error = err
		call.done()
		return
	}

	//刷新复用的header
	c.h.ServiceMethod = call.ServiceMethod
	c.h.Seq = seq
	c.h.Err = ""

	// 往编解码器写入header和args
	if err := c.cc.Write(&c.h, call.Args); err != nil {
		call = c.removeCall(seq)
		if call != nil {
			call.Error = err
			call.done()
		}
	}
}

// call操作相关
func (c *Client) registerCall(call *Call) (uint64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closing || c.shutdown {
		return 0, ErrShutdown
	}
	seq := c.seq
	c.seq++
	c.pendingCalls[seq] = call

	return seq, nil
}

func (c *Client) removeCall(seq uint64) *Call {
	c.mu.Lock()
	defer c.mu.Unlock()
	call := c.pendingCalls[seq]
	delete(c.pendingCalls, seq)
	return call
}

func (c *Client) terminateCalls(err error) {
	c.reqMu.Lock()
	defer c.reqMu.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.shutdown = true
	for _, call := range c.pendingCalls {
		call.Error = err
		call.done()
	}
}
