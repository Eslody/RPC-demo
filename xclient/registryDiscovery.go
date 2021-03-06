package xclient
//支持注册中心的服务发现模块
import (
	"log"
	"net/http"
	"strings"
	"time"
)

type GonRegistryDiscovery struct {
	*MultiServersDiscovery
	registry string			//注册中心地址
	timeout time.Duration	//过期时间
	lastUpdate time.Time	//最后更新时间
}

//默认过期时间
const defaultUpdateTimeout = time.Second * 10

func NewGonRegistryDiscovery(registryAddr string, timeout time.Duration) *GonRegistryDiscovery {
	if timeout == 0 {
		timeout = defaultUpdateTimeout
	}
	d := &GonRegistryDiscovery{
		MultiServersDiscovery: NewMultiServerDiscovery(make([]string, 0)),
		registry: registryAddr,
		timeout: timeout,
	}
	return d
}

func (d *GonRegistryDiscovery) Update(servers []string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.servers = servers
	d.lastUpdate = time.Now()
	return nil
}

//为保证准确性，先更新后再获取调用server
func (d *GonRegistryDiscovery) Get(mode SelectMode) (string, error) {
	if err := d.Refresh(); err != nil {
		return "", err
	}
	return d.MultiServersDiscovery.Get(mode)
}

func (d *GonRegistryDiscovery) GetAll() ([]string, error) {
	if err := d.Refresh(); err != nil {
		return nil, err
	}
	return d.MultiServersDiscovery.GetAll()
}

//从注册中心处更新servers
func (d *GonRegistryDiscovery) Refresh() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	//未过期直接返回
	if d.lastUpdate.Add(d.timeout).After(time.Now()) {
		return nil
	}
	//更新
	log.Println("rpc registry: refresh servers from registry", d.registry)
	resp, err := http.Get(d.registry)
	if err != nil {
		log.Println("rpc registry refresh err:", err)
		return err
	}

	servers := strings.Split(resp.Header.Get("X-RPCServers"), ",")
	d.servers = make([]string, 0, len(servers))
	for _, server := range servers {
		if strings.TrimSpace(server) != "" {
			d.servers = append(d.servers, strings.TrimSpace(server))
		}
	}
	d.lastUpdate = time.Now()
	return nil
}