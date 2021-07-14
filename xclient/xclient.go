package xclient

import (
	"context"
	. "gonrpc"
	"reflect"
	"sync"
)

type XClient struct {
	d Discovery
	mode SelectMode
	opt *Option	//需要用户提供option
	mu sync.Mutex
	clients map[string]*Client  //格式：protocol@addr - *client
}


func NewXClient(d Discovery, mode SelectMode, opt *Option) *XClient {
	return &XClient{
		d: d,
		mode: mode,
		opt: opt,
		clients: make(map[string]*Client),
	}
}

func (xc *XClient) Close() error {
	xc.mu.Lock()
	defer xc.mu.Unlock()
	for key, client := range xc.clients {
		_ = client.Close()
		delete(xc.clients, key)
	}
	return nil
}
//检查是否有缓存且可用的Client
func (xc *XClient) dial(rpcAddr string) (*Client, error) {
	xc.mu.Lock()
	defer xc.mu.Unlock()
	client, ok := xc.clients[rpcAddr]
	if ok && !client.IsAvailable() {
		_ = client.Close()
		delete(xc.clients, rpcAddr)
		client = nil
	}
	if client == nil {
		var err error
		client, err = XDial(rpcAddr, xc.opt)
		if err != nil {
			return nil, err
		}
		xc.clients[rpcAddr] = client
	}
	return client, nil
}

func (xc *XClient) call(rpcAddr string, ctx context.Context, serviceMethod string, args, reply interface{}) error {
	client, err := xc.dial(rpcAddr)
	if err != nil {
		return nil
	}
	return client.Call(ctx, serviceMethod, args, reply)

}
//XClient用负载均衡策略去服务发现模块找到一个服务并寻找/建立与之的客户端连接
func (xc *XClient) Call(ctx context.Context, serviceMethod string, args, reply interface{}) error {
	rpcAddr, err := xc.d.Get(xc.mode)
	if err != nil {
		return err
	}
	return xc.call(rpcAddr, ctx, serviceMethod, args, reply)
}
//广播表示服务发现中所有的server都要执行RPC
func (xc *XClient) Broadcast(ctx context.Context, serviceMethod string, args, reply interface{}) error {
	servers, err := xc.d.GetAll()
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var e error
	//调用是否结束：如果reply是nil，表示调用者不想返回结果，因此不需要set调用结果
	replyDone := reply == nil
	ctx, cancel := context.WithCancel(ctx)
	for _, rpcAddr := range servers {
		wg.Add(1)
		go func(rpcAddr string) {
			defer wg.Done()
			var clonedReply interface{}
			if reply != nil {
				clonedReply = reflect.New(reflect.ValueOf(reply).Elem().Type()).Interface()
			}
			err := xc.call(rpcAddr, ctx, serviceMethod, args, clonedReply)
			//使用互斥锁保证 error 和 reply 能被正确赋值
			mu.Lock()
			//如果调用失败，取消当前的call
			if err != nil && e == nil {
				e = err
				//调用cancel函数即主动取消
				cancel()
			}
			//调用成功则set结果
			if err == nil && !replyDone {
				reflect.ValueOf(reply).Elem().Set(reflect.ValueOf(clonedReply).Elem())
				replyDone = true
			}
			mu.Unlock()
		}(rpcAddr)
	}
	wg.Wait()

	return e
}