package main

import (
	gee_rpc "github.com/futhergo/gee-rpc"
	"log"
	"net"
	"sync"
	"time"
)

type Foo int

type Args struct{ Num1, Num2 int }

func (f Foo) Sum(args Args, reply *int) error {
	*reply = args.Num1 + args.Num2
	return nil
}


func startServer(addr chan string) {
	// pick a free port
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		log.Fatal("network error:", err)
	}
	log.Println("start rpc server on", l.Addr())
	var f Foo
	gee_rpc.Register(&f)
	addr <- l.Addr().String()
	gee_rpc.Accept(l)
}

func main() {
	addr := make(chan string)
	go startServer(addr)

	// in fact, following code is like a simple geerpc client
	client, _ := gee_rpc.Dial("tcp", <-addr)
	defer func() { _ = client.Close() }()

	time.Sleep(time.Second)
	// send request & receive response
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			args := &Args{Num1: i, Num2: i * i}
			var reply int
			err := client.CallMethod("Foo.Sum", args, &reply)
			if err != nil {
				log.Fatalf("remote produce call failed %v", err)
			}
			log.Printf("%d + %d = %d", args.Num1, args.Num2, reply)
			wg.Done()
		}(i)
	}
	wg.Wait()
}