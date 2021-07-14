package gonrpc
//客户端
import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"gonrpc/codec"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
)
//封装了Body
type Call struct {
	Seq           uint64
	ServiceMethod string
	Args          interface{}
	Reply         interface{}
	Error         error
	Done          chan *Call
}

func (call *Call) done() {
	call.Done <- call
}

type Client struct {
	cc       codec.Codec
	opt      *Option
	sending  sync.Mutex				//发送Call锁
	mu       sync.Mutex				//访问Call锁
	header   codec.Header			//由Client发送每次的header+Call(Body)，为节省建header的时间直接将其放到Client内
	seq      uint64					//目前增长截止的编号
	pending  map[uint64]*Call		//存储未处理完的请求
	closing  bool 					//用户调用结束
	shutdown bool 					//服务端终止服务
}

var ErrShutdown = errors.New("connection is shut down")

//关闭连接
func (client *Client) Close() error {
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.closing {
		return ErrShutdown
	}
	client.closing = true
	return client.cc.Close()
}

func (client *Client) IsAvailable() bool {
	client.mu.Lock()
	defer client.mu.Unlock()
	return !client.shutdown && !client.closing
}

func (client *Client) registerCall(call *Call) (uint64, error) {
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.closing || client.shutdown {
		return 0, ErrShutdown
	}
	call.Seq = client.seq
	client.pending[call.Seq] = call
	client.seq++
	return call.Seq, nil
}

func (client *Client) removeCall(seq uint64) *Call {
	client.mu.Lock()
	defer client.mu.Unlock()
	call := client.pending[seq]
	delete(client.pending, seq)
	return call
}

//停掉所有Call
func (client *Client) terminateCalls(err error) {
	client.sending.Lock()
	defer client.sending.Unlock()

	client.mu.Lock()
	defer client.mu.Unlock()
	client.shutdown = true
	for _, call := range client.pending {
		call.Error = err
		call.done()
	}
}

func (client *Client) send(call *Call) {
	//确保请求发送完整
	client.sending.Lock()
	defer client.sending.Unlock()

	//call注册
	seq, err := client.registerCall(call)
	if err != nil {
		call.Error = err
		call.done()
		return
	}

	//header
	client.header.ServiceMethod = call.ServiceMethod
	client.header.Seq = seq
	client.header.Error = ""

	if err := client.cc.Write(&client.header, call.Args); err != nil {
		call := client.removeCall(seq)
		if call != nil {
			call.Error = err
			call.done()
		}
	}
}

func (client *Client) receive() {
	var err error
	for err == nil {
		var h codec.Header
		if err = client.cc.ReadHeader(&h); err != nil {
			break
		}
		call := client.removeCall(h.Seq)
		switch {

		case call == nil:
			//通常表示因之前产生的写入错误导致call已经被移除
			err = client.cc.ReadBody(nil)
			break

		case h.Error != "":
			call.Error = fmt.Errorf(h.Error)
			err = client.cc.ReadBody(nil)
			call.done()
			break

		default:
			err = client.cc.ReadBody(call.Reply)
			if err != nil {
				call.Error = errors.New("reading body " + err.Error())
			}
			call.done()
		}
	}

	client.terminateCalls(err)
}

//由客户端决定具体每次调用的超时时间
func (client *Client) Call(ctx context.Context, serviceMethod string, args, reply interface{}) error {
	call := &Call{
		ServiceMethod: serviceMethod,
		Args:          args,
		Reply:         reply,
		Done:          make(chan *Call, 1),
	}
	go client.send(call)
	select {
	//客户端调用方法超时
	case <-ctx.Done():
		client.removeCall(call.Seq)
		return errors.New("rpc client: call failed: " + ctx.Err().Error())
	//正常完成
	case cur := <-call.Done:
		return cur.Error
	}
}

//添加option参数，nil即默认option
func handleOption(opt *Option) (*Option, error) {
	if opt == nil {
		return DefaultOption, nil
	}
	opt.MagicNumber = DefaultOption.MagicNumber
	if opt.CodecType == "" {
		opt.CodecType = DefaultOption.CodecType
	}
	return opt, nil
}

//新建支持http的客户端，对于客户端来说还是封装rpc结构进行远程调用
func NewHTTPClient(conn net.Conn, opt *Option) (*Client, error) {
	//采用HTTP代理的方式
	io.WriteString(conn, fmt.Sprintf("CONNECT %s HTTP/1.0\n\n", defaultRPCPath))
	resp, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: "CONNECT"})

	if err == nil && resp.Status == connected {
		return NewClient(conn, opt)
	}
	if err == nil {
		err =errors.New("unexpected HTTP response: " + resp.Status)
	}
	return nil, err
}

func NewClient(conn net.Conn, opt *Option) (*Client, error) {
	//序列化器构造方法
	f := codec.NewCodecFuncMap[opt.CodecType]
	if f == nil {
		err := fmt.Errorf("invalid codec type %s", opt.CodecType)
		log.Println("rpc client: codec error:", err)
		return nil, err
	}

	//商议用户自定义rpc格式，用json格式传递
	if err := json.NewEncoder(conn).Encode(opt); err != nil {
		log.Println("rpc client: options error: ", err)
		return nil, err
	}
	return newClientCodec(f(conn), opt), nil
}

//函数作参数，其他可能超时的参数见test
type newClientFunc func(conn net.Conn, opt *Option) (*Client, error)

func newClientCodec(cc codec.Codec, opt *Option) *Client {
	client := &Client{
		seq:     1, //seq从1开始，0表示无效call
		cc:      cc,
		opt:     opt,
		pending: make(map[uint64]*Call),
	}
	go client.receive()
	return client
}

//建立连接
func XDial(rpcAddr string, opt *Option) (client *Client, err error) {
	parts := strings.Split(rpcAddr, "@")
	if len(parts) != 2 {
		return nil, fmt.Errorf("rpc client err: wrong format '%s', expect protocol@addr", rpcAddr)
	}
	protocol, addr := parts[0], parts[1]
	switch protocol {
	//支持http
	case "http":
		return dialTimeout(NewHTTPClient, "tcp", addr, opt)
	//tcp, unix或其他传输协议
	default:
		return dialTimeout(NewClient, protocol, addr, opt)
	}
}

func dialTimeout(f newClientFunc, network, address string, option *Option) (client *Client, err error) {
	opt, err := handleOption(option)
	if err != nil {
		return nil, err
	}

	//连接创建超时
	conn, err := net.DialTimeout(network, address, opt.ConnectTimeout)
	if err != nil {
		return nil, err
	}
	//生成client失败即关闭连接
	defer func() {
		if client == nil {
			_ = conn.Close()
		}
	}()

	//多种构造方式
	return f(conn, opt)

}
