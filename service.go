package gee_rpc

import (
	"go/ast"
	"go/token"
	"log"
	"reflect"
	"sync/atomic"
)

type methodType struct {
	method reflect.Method
	ArgType reflect.Type
	ReplyType reflect.Type
	numCalls uint64
}

func (m *methodType)NumsCall() uint64 {
	return atomic.LoadUint64(&m.numCalls)
}

func (m *methodType)newArgv() reflect.Value {
	if m.ArgType.Kind() == reflect.Ptr {
		return reflect.New(m.ArgType.Elem())
	}
	return reflect.New(m.ArgType).Elem()
}

func (m *methodType)newReplyv() reflect.Value {
	elemT := m.ReplyType.Elem()
	replyV := reflect.New(elemT)
	switch elemT.Kind() {
	case reflect.Map:
		replyV.Elem().Set(reflect.MakeMap(elemT))
	case reflect.Slice:
		replyV.Elem().Set(reflect.MakeSlice(elemT, 0, 0))
	}
	return replyV
}

type service struct {
	name string
	typ reflect.Type
	rcvr reflect.Value
	method map[string]*methodType
}

func newService(rcvr interface{}) *service {
	s := new(service)
	s.rcvr = reflect.ValueOf(rcvr)
	s.name = reflect.Indirect(s.rcvr).Type().Name()
	s.typ = reflect.TypeOf(rcvr)
	if !ast.IsExported(s.name) {
		log.Fatalf("%s is not exported type", s.name)
	}
	s.registerMethods()
	return s
}

var typeOfError =  reflect.TypeOf((*error)(nil)).Elem()

func isExportedOrBuiltinType(t reflect.Type) bool {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	// PkgPath will be non-empty even for an exported type,
	// so we need to check the type name as well.
	return token.IsExported(t.Name()) || t.PkgPath() == ""
}

func (s *service)registerMethods() {
	s.method = make(map[string]*methodType)
	for i := 0; i<s.typ.NumMethod(); i++ {
		method := s.typ.Method(i)
		mType := method.Type
		if mType.NumIn() != 3 {
			log.Printf("%s's method %s's in args nums is not 3", s.name, method.Name)
			continue
		}
		if mType.NumOut() != 1 {
			log.Printf("%s's method %s's out args nums is not 1", s.name, method.Name)
			continue
		}
		outType := mType.Out(0)
		if outType != typeOfError {
			log.Printf("%s's method %s's out args %v is not error type", s.name, method.Name, outType)
			continue
		}
		argType := mType.In(1)
		if !isExportedOrBuiltinType(argType) {
			log.Printf("%s's method %s's in argType %v is not exported type", s.name, method.Name, argType)
			continue
		}
		replyType := mType.In(2)
		if replyType.Kind() != reflect.Ptr {
			log.Printf("%s's method %s's in replyType %v is not pointer type", s.name, method.Name, replyType)
			continue
		}
		if !isExportedOrBuiltinType(replyType) {
			log.Printf("%s's method %s's in replyType %v is not exported type", s.name, method.Name, replyType)
			continue
		}
		s.method[method.Name] = &methodType{
			method: method,
			ArgType: argType,
			ReplyType: replyType,
		}
	}
}

func (s *service)call(m *methodType, argv, replyv reflect.Value) error {
	atomic.AddUint64(&m.numCalls, 1)
	f := m.method.Func
	rv := f.Call([]reflect.Value{s.rcvr, argv, replyv})
	if outV := rv[0].Interface(); outV != nil {
		return outV.(error)
	}
	return nil
}