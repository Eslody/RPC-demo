package xclient
//客户端服务发现模块
import (
	"errors"
	"math"
	"math/rand"
	"sync"
	"time"
)

//负载均衡策略
type SelectMode int

const (
	RandomSelect SelectMode = iota			//随机策略
	RoundRobinSelect						//轮询策略
)

type Discovery interface {
	Refresh() error							//从注册中心更新服务列表
	Update(servers []string) error  		//手动更新
	Get(mode SelectMode) (string, error)  	//根据负载均衡策略选择一个服务实例
	GetAll() ([]string, error)
}

//采用手工维护的服务发现模块
type MultiServersDiscovery struct {
	r *rand.Rand  							//生成随机数
	mu sync.RWMutex  						//为提高并发量采用读写锁
	servers []string						//当前可用服务
	index int 								//记录轮询算法选择的位置
}

func NewMultiServerDiscovery(servers []string) *MultiServersDiscovery{
	d := &MultiServersDiscovery{
		servers: servers,
		r: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
	d.index = d.r.Intn(math.MaxInt32 - 1)  //避免每次从0开始，初始化时随机设定一个值
	return d
}

//检测实现某接口
var _ Discovery = (*MultiServersDiscovery)(nil)

func (d *MultiServersDiscovery) Refresh() error {
	return nil
}
//如果没有注册中心只能这样手动更新
func (d *MultiServersDiscovery) Update(servers []string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.servers = servers
	return nil
}

func (d *MultiServersDiscovery) Get(mode SelectMode) (string, error) {
	//为增大并发量，这里用读锁
	d.mu.RLock()
	defer d.mu.RUnlock()
	n := len(d.servers)
	if n == 0 {
		return "", errors.New("rpc discovery: no available servers")
	}
	switch mode {
	case RandomSelect:
		return d.servers[d.r.Intn(n)], nil
	case RoundRobinSelect:
		s := d.servers[d.index%n]
		d.index = (d.index + 1) % n
		return s, nil
	default:
		return "", errors.New("rpc discovery: not supported select mode")
	}
}

func (d *MultiServersDiscovery) GetAll() ([]string, error) {
	//为增大并发量，这里用读锁
	d.mu.RLock()
	defer d.mu.RUnlock()

	servers := make([]string, len(d.servers))
	copy(servers, d.servers)
	return servers, nil
}