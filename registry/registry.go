package registry
//注册中心
import (
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

//GonRegistry 是一个简单的注册中心，实现了服务注册和心跳保活机制
//返回所有存活的server并更新不可用的server，默认情况5min后服务即进入不可用状态
type GonRegistry struct {
	timeout time.Duration	//超时时间：为0表示不设限制
	mu sync.Mutex
	servers map[string]*ServerItem
}

//服务器地址和起始时间
type ServerItem struct {
	Addr string
	start time.Time
}

//采用http协议提供服务
const (
	defaultPath = "/_gonrpc_/registry"
	defaultTimeout = time.Minute * 5
)

func NewRegistry(timeout time.Duration) *GonRegistry {
	return &GonRegistry {
		timeout: timeout,
		servers: make(map[string]*ServerItem),
	}
}

var DefaultGonRegistry = NewRegistry(defaultTimeout)

//添加服务实例
func (r *GonRegistry) putServer(addr string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.servers[addr]
	if s == nil {
		r.servers[addr] = &ServerItem{Addr: addr, start: time.Now()}
	} else {
		//若存在则更新起始时间以保活
		s.start = time.Now()
	}
}

//返回可用的服务列表，格式为服务的地址
func (r *GonRegistry) aliveServers() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var alive []string
	for addr, s := range r.servers {
		if r.timeout == 0 || s.start.Add(r.timeout).After(time.Now()) {
			alive = append(alive, addr)
		} else {
			delete(r.servers, addr)
		}
	}
	return alive
}

//在defaultPath处提供服务，为简单所有的信息都放到Header中
func (r *GonRegistry) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	//返回所有可用的服务列表
	case "GET":
		w.Header().Set("X-RPCServers", strings.Join(r.aliveServers(), ","))
	//添加服务实例或发送心跳
	case "POST":
		addr := req.Header.Get("X-RPCServer")
		if addr == "" {
			w.WriteHeader(http.StatusInternalServerError)//500
			return
		}
		r.putServer(addr)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)//405
	}
}

func (r *GonRegistry) HandleHTTP(registryPath string) {
	http.Handle(registryPath, r)
	log.Println("rpc registry path:", registryPath)
}

func HandleHTTP() {
	DefaultGonRegistry.HandleHTTP(defaultPath)
}

//用于注册server/心跳保活，服务启动时定时向注册中心发送心跳
func Heartbeat(registry, addr string, duration time.Duration) {
	if duration == 0 {
		//默认周期比注册中心设置的过期时间少1min，保证在注册中心移除server前有充足时间发送心跳
		duration = defaultTimeout - 1 * time.Minute
	}
	//初始注册
	err := sendHeartbeat(registry, addr)
	//定期保活
	go func() {
		t := time.NewTicker(duration)
		for err == nil {
			<-t.C
			err = sendHeartbeat(registry, addr)
		}
	}()
}

//发送心跳
func sendHeartbeat(registry, addr string) error {
	log.Println(addr, "send heart beat to registry", registry)
	client := &http.Client{}
	req, _ := http.NewRequest("POST", registry, nil)
	req.Header.Set("X-RPCServer", addr)
	if _, err := client.Do(req); err != nil {
		log.Println("rpc server: heart beat err:", err)
		return err
	}
	return nil
}