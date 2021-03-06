package main

import (
	"context"
	"gonrpc"
	"gonrpc/registry"
	"gonrpc/xclient"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

type Foo int

type Args struct{ Num1, Num2 int }

func (f Foo) Sum(args Args, reply *int) error {
	*reply = args.Num1 + args.Num2
	return nil
}

func (f Foo) Sleep(args Args, reply *int) error {
	time.Sleep(time.Second * time.Duration(args.Num1))
	*reply = args.Num1 + args.Num2
	return nil
}
//启动注册中心，在本机8000端口
func startRegistry(wg *sync.WaitGroup) {
	l, _ := net.Listen("tcp", ":8000")
	registry.HandleHTTP()
	wg.Done()
	http.Serve(l, nil)
}

//启动服务并注册在注册中心，通过端口模拟多集群
func startServer(registryAddr string, wg *sync.WaitGroup) {
	var foo Foo
	l, _ := net.Listen("tcp", ":0")
	server := gonrpc.NewServer()
	_ = server.Register(&foo)
	registry.Heartbeat(registryAddr, "tcp@"+l.Addr().String(), 0)
	wg.Done()

	server.Accept(l)
}

func startHTTPServer(registryAddr string, wg *sync.WaitGroup) {
	defer wg.Done()
	var foo Foo
	l, _ := net.Listen("tcp", ":9999")
	_ = gonrpc.Register(&foo)
	gonrpc.HandleHTTP()
	registry.Heartbeat(registryAddr, "http@"+l.Addr().String(), 0)

	_ = http.Serve(l, nil)
}

func foo(xc *xclient.XClient, ctx context.Context, typ, serviceMethod string, args *Args) {
	var reply int
	var err error
	switch typ {
	case "call":
		err = xc.Call(ctx, serviceMethod, args, &reply)
	case "broadcast":
		err = xc.Broadcast(ctx, serviceMethod, args, &reply)
	}
	if err != nil {
		log.Printf("%s %s error: %v", typ, serviceMethod, err)
	} else {
		log.Printf("%s %s success: %d + %d = %d", typ, serviceMethod, args.Num1, args.Num2, reply)
	}
}

func call(registry string) {
	d := xclient.NewGonRegistryDiscovery(registry, 0)
	xc := xclient.NewXClient(d, xclient.RandomSelect, nil)
	defer func() { _ = xc.Close() }()

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			foo(xc, context.Background(), "call", "Foo.Sum", &Args{Num1: i, Num2: i * i})

		}(i)
	}
	wg.Wait()
}

func broadcast(registry string) {
	d := xclient.NewGonRegistryDiscovery(registry, 0)
	xc := xclient.NewXClient(d, xclient.RandomSelect, nil)
	defer func() { _ = xc.Close() }()

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			foo(xc, context.Background(), "broadcast", "Foo.Sum", &Args{Num1: i, Num2: i * i})
			//2 - 5 timeout
			ctx, _ := context.WithTimeout(context.Background(), time.Second*2)
			foo(xc, ctx, "broadcast", "Foo.Sleep", &Args{Num1: i, Num2: i * i})
		}(i)
	}
	wg.Wait()
}

func main() {
	log.SetFlags(0)
	//注册中心地址
	registryAddr := "http://localhost:8000/_gonrpc_/registry"
	var wg sync.WaitGroup
	wg.Add(1)
	go startRegistry(&wg)
	wg.Wait()
	//服务注册
	wg.Add(2)
	go startServer(registryAddr, &wg)
	go startServer(registryAddr, &wg)
	wg.Wait()
	//开启http服务端
	wg.Add(1)
	go startHTTPServer(registryAddr, &wg)

	//
	call(registryAddr)
	broadcast(registryAddr)
	wg.Wait()
}