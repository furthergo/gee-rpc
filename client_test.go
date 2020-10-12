package gee_rpc

import (
	"net"
	"os"
	"runtime"
	"testing"
	"time"
)

func TestClient_dialTimeout(t *testing.T) {
	t.Parallel()
	l, _ := net.Listen("tcp", ":0")


	t.Run("timeout", func(t *testing.T) {
		_, err := Dial("tcp", l.Addr().String(), &Option{ConnTimeOut: time.Second})
		_assert(err != nil, "expect a timeout error")
	})
	t.Run("0", func(t *testing.T) {
		_, err := Dial("tcp", l.Addr().String(), &Option{ConnTimeOut: 0})
		_assert(err == nil, "0 means no limit")
	})
}

func TestXDial(t *testing.T) {
	ch := make(chan struct{})
	t.Log(runtime.GOOS)
	addr := "/tmp/geerpc.sock"
	go func() {
		_ = os.Remove(addr)
		l, err := net.Listen("unix", addr)
		if err != nil {
			t.Fatalf("listen local socket failed")
		}
		ch <- struct{}{}
		Accept(l)
	}()
	<- ch
	_, err := XDial("unix@" + addr)
	_assert(err == nil, "dial socket failed")
}