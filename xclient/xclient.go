package xclient

import (
	"context"
	. "github.com/futhergo/gee-rpc"
	"reflect"
	"sync"
)

type SelectMode int

const (
	RandomSelect SelectMode = iota
	RoundRobinSelect
)

type Discovery interface {
	Refresh() error
	Update([]string) error
	Get(mode SelectMode) (string, error)
	GetAll() ([]string, error)
}


type XClient struct {
	clients map[string]*Client // rpcAddr, *Client
	d Discovery
	mu sync.Mutex
	mode SelectMode
	opt *Option
}

func NewXClient(d Discovery, m SelectMode, opt *Option) *XClient {
	return &XClient{
		clients: make(map[string]*Client),
		d: d,
		mode: m,
		opt: opt,
	}
}

func (xc *XClient)Close() error {
	xc.mu.Lock()
	defer xc.mu.Unlock()
	for k, c := range xc.clients {
		_ = c.Close()
		delete(xc.clients, k)
	}
	return nil
}

func (xc *XClient)Call(ctx context.Context, serviceMethod string, args, reply interface{}) error {
	// Discovery: 选择节点，rpcAddr
	// 连接rpcAddr, 返回client
	// client调用serviceMethod
	rpcAddr, err := xc.d.Get(xc.mode)
	if err != nil {
		return err
	}
	return xc.call(rpcAddr, ctx, serviceMethod, args, reply)
}

func (xc *XClient)Broadcast(ctx context.Context, serviceMethod string, args, reply interface{}) error {
	servers, err := xc.d.GetAll()
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var anyErr error
	replyUpdated := reply == nil
	ctx, cancel := context.WithCancel(ctx) // cancel context
	for _, addr := range servers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var midReply interface{}
			if !replyUpdated {
				midReply = reflect.New(reflect.ValueOf(reply).Elem().Type()).Interface()
			}

			err := xc.call(addr, ctx, serviceMethod, args, midReply)
			mu.Lock()
			if err != nil && anyErr == nil { // 更新err
				anyErr = err
				cancel() // 出错，调用cancel，ctx会直接调用Done，结束call
			}
			if err == nil && !replyUpdated { // 更新reply
				reflect.ValueOf(reply).Elem().Set(reflect.ValueOf(midReply).Elem())
				replyUpdated = true
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	return anyErr
}

func (xc *XClient)dial(rpcAddr string) (*Client, error) {
	xc.mu.Lock()
	defer xc.mu.Unlock()
	c, ok := xc.clients[rpcAddr]
	if ok && !c.IsAvailable() {
		_ = c.Close()
		delete(xc.clients, rpcAddr)
		c = nil
	}
	if c != nil {
		return c, nil
	}
	c, err := XDial(rpcAddr, xc.opt)
	if err != nil {
		return nil, err
	}
	xc.clients[rpcAddr] = c
	return c, nil
}

func (xc *XClient)call(rpcAddr string, ctx context.Context, serviceMethod string, args, reply interface{}) error {
	c, err := xc.dial(rpcAddr)
	if err != nil {
		return err
	}
	return  c.CallMethod(ctx, serviceMethod, args, reply)
}
