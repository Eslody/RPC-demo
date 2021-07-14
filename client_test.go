package gonrpc

import (
	"context"
	"log"
	"net"
	"strings"
	"testing"
	"time"
)
//case 1  客户端连接超时
func TestClient_dialTimeout(t *testing.T) {
	t.Parallel()
	t.Run("timeout", func(t *testing.T) {
		_, err := XDial("tcp@facebook.com:80", &Option{ConnectTimeout: time.Nanosecond})
		_assert(err != nil && strings.Contains(err.Error(), "timeout"), "expect a timeout error")
	})

	t.Run("timeout", func(t *testing.T) {
		_, err := XDial("tcp@facebook.com:80", &Option{ConnectTimeout: 0})
		_assert(err == nil, "0 means no limit")
	})
}
//case 2  服务端处理超时
type Bar int
func (b Bar) Timeout(argv int, reply *int) error {
	time.Sleep(2 * time.Second)
	return nil
}

func startServer(addr chan string) {
	var b Bar
	if err := Register(&b); err != nil {
		log.Fatal("register error:", err)
	}
	l, _ := net.Listen("tcp", ":0")
	addr <- l.Addr().String()
	Accept(l)
}

func TestClient_Call(t *testing.T) {
	t.Parallel()
	addrh := make(chan string)
	go startServer(addrh)
	addr := <-addrh
	time.Sleep(time.Second)
	t.Run("client timeout", func(t *testing.T) {
		client, _ := XDial("tcp@" + addr, nil)
		ctx, _ := context.WithTimeout(context.Background(), time.Second)//表示经过1s超时
		var reply int
		err := client.Call(ctx, "Bar.Timeout", 1, &reply)
		_assert(err != nil && strings.Contains(err.Error(), ctx.Err().Error()), "expect a timeout error")
	})
	t.Run("server handle timeout", func(t *testing.T) {
		client, _ := XDial("tcp@" + addr, &Option{
			HandleTimeout: time.Second,
		})
		var reply int
		err := client.Call(context.Background(), "Bar.Timeout", 1, &reply)
		_assert(err != nil && strings.Contains(err.Error(), "handle timeout"), "expect a timeout error")
	})
}
