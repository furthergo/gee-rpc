package gee_rpc

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/futhergo/gee-rpc/codec"
	"io"
	"log"
	"net"
	"reflect"
	"strings"
	"sync"
)

const MagicNumber = 0x9a7ccda

type Option struct {
	MagicNumber int
	CodecType codec.Type
}

var DefaultOption = &Option{
	MagicNumber: MagicNumber,
	CodecType: codec.GobType,
}

type Server struct {
	serviceMap sync.Map
}

func NewServer() *Server {
	return &Server{}
}

var DefaultServer = NewServer()

func Accept(l net.Listener) {
	DefaultServer.Accept(l)
}

func Register(rcsv interface{}) error {
	return DefaultServer.Register(rcsv)
}

func (s *Server)Accept(l net.Listener) {
	// 传入listener
	for {
		conn, err := l.Accept() // 建立tcp connect
		if err != nil {
			log.Printf("connect accept failed %v", err)
			continue
		}
		go s.ServeConn(conn) // 开启协程处理connect
	}
}

func (s *Server)Register(rcsv interface{}) error {
	svc := newService(rcsv)
	if _, dup := s.serviceMap.LoadOrStore(svc.name, svc); dup {
		log.Fatalf("duplicate register service of %v", rcsv)
		return errors.New(fmt.Sprintf("duplicate register service of %v", rcsv))
	}
	return nil
}

func (s *Server)findService(serviceMethod string) (*service, *methodType, error) {
	dotIdx := strings.Index(serviceMethod, ".")
	if dotIdx <0 {
		return nil, nil, errors.New("error format of call service method")
	}
	serviceName, methodName := serviceMethod[:dotIdx], serviceMethod[dotIdx+1:]
	svci, ok := s.serviceMap.Load(serviceName)
	if !ok {
		return nil, nil, errors.New(fmt.Sprintf("non register service of %s", serviceName))
	}
	svc := svci.(*service)
	mType, ok := svc.method[methodName]
	if !ok {
		return nil, nil, errors.New(fmt.Sprintf("%s not found method %s", serviceName, methodName))
	}
	return svc, mType, nil
}

func (s *Server)ServeConn(conn io.ReadWriteCloser) {
	defer func() {
		_ = conn.Close()
	}()
	var opt Option
	if err := json.NewDecoder(conn).Decode(&opt); err != nil { // 解析option，
		log.Printf("decode option failed: %v", err)
		return
	}
	if opt.MagicNumber != MagicNumber { // 判断MagicNumber
		log.Printf("option magic number error %d", opt.MagicNumber)
		return
	}
	f := codec.NewCodecFuncMap[opt.CodecType] // 获取对应codecType的func
	if f == nil {
		log.Printf("null register codec func of %v", opt.CodecType)
		return
	}
	// 用conn初始化对应的编解码器Codec
	// 开始处理Codec
	s.serveCodec(f(conn))
}

var invalidRequest= struct {}{}

func (s *Server)serveCodec(cc codec.Codec) {
	mu := &sync.Mutex{} // 并发锁，多个handleRequest的goroutine同时间只允许一个在sendResponse，即向tcp连接写response
	wg := &sync.WaitGroup{} // waitGroup，等待所有的handleRequest处理完成
	for {
		req, err := s.readRequest(cc) // 尝试从tcp中读取request并反序列化
		if err != nil {
			if req == nil {
				break
			}
			req.h.Err = err.Error()
			s.sendResponse(cc, req.h, invalidRequest, mu)
			continue
		}
		wg.Add(1)
		go s.handleRequest(cc, req, mu, wg) // 处理request，传入并发锁控制多个request并发写response；waitGroup，通知关闭编解码器cc.Close()
	}
	wg.Wait()
	_ = cc.Close()
}

type request struct {
	h *codec.Header
	argv, replyv reflect.Value
	svc *service
	mType *methodType
}

func (s *Server)readRequest(cc codec.Codec) (*request, error) {
	h, err := s.readRequestHeader(cc) // 从tcp连接中读取header并反序列化
	if err != nil { // 如果失败，返回request为空，代表无法读取到header
		return nil, err
	}
	req := &request{
		h: h,
	}
	req.svc, req.mType, err = s.findService(h.ServiceMethod)
	if err != nil {
		return req, err
	}
	req.argv, req.replyv = req.mType.newArgv(), req.mType.newReplyv()
	argvi := req.argv.Interface()
	if req.argv.Type().Kind() != reflect.Ptr {
		argvi = req.argv.Addr().Interface()
	}
	if err = cc.ReadBody(argvi); err != nil { // 解析参数, 失败尝试也返回request
		log.Printf("read request argv failed %v", err)
	}
	return req, nil
}

func (s *Server)readRequestHeader(cc codec.Codec) (*codec.Header, error) {
	var ch codec.Header
	if err := cc.ReadHeader(&ch); err != nil { // 读取header，用gob.decode
		return nil, err
	}
	return &ch, nil
}

func (s *Server)handleRequest(cc codec.Codec, r *request, mu *sync.Mutex, wg *sync.WaitGroup) {
	defer wg.Done() // 通知当前request处理完成
	err := r.svc.call(r.mType, r.argv, r.replyv)
	if err != nil {
		r.h.Err = err.Error()
		s.sendResponse(cc, r.h, invalidRequest, mu)
		return
	}
	s.sendResponse(cc, r.h, r.replyv.Interface(), mu) // 写入连接
}

func (s *Server)sendResponse(cc codec.Codec, h *codec.Header, body interface{}, mu *sync.Mutex) {
	mu.Lock() // 互斥处理
	defer mu.Unlock()
	if err := cc.Write(h, body); err != nil { // 用gob.encode序列化header和body，然后写入
		log.Printf("write response body failed %v", err)
	}
}