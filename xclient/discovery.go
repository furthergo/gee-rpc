package xclient

import (
	"errors"
	"log"
	"math"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"
)

type MultiServerDiscovery struct {
	r *rand.Rand
	mu sync.Mutex
	servers []string
	idx int
}

func NewMultiServerDiscovery(servers []string) *MultiServerDiscovery {
	m := &MultiServerDiscovery{
		r: rand.New(rand.NewSource(time.Now().UnixNano())),
		servers: servers,

	}
	m.idx = m.r.Intn(math.MaxInt32 - 1)
	return m
}

func (m *MultiServerDiscovery) Refresh() error {
	return nil
}

func (m *MultiServerDiscovery) Update(strings []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.servers = strings
	return nil
}

func (m *MultiServerDiscovery) Get(mode SelectMode) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := len(m.servers)
	if n == 0 {
		return "", errors.New("empty server node")
	}
	switch mode {
	case RandomSelect:
		return m.servers[m.r.Intn(n)], nil
	case RoundRobinSelect:
		s := m.servers[m.idx%n]
		m.idx = (m.idx + 1)%n
		return s, nil
	default:
		return "", errors.New("false select mode")
	}
}

func (m *MultiServerDiscovery) GetAll() ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cpS := make([]string, len(m.servers), len(m.servers))
	copy(cpS, m.servers)
	return cpS, nil
}

// 持有MultiServerDiscovery，从注册中心registry注册和刷新servers
type GeeRegistryDiscovery struct {
	*MultiServerDiscovery
	registry string // 地址
	timeout time.Duration // 刷新间隔
	lastUpdate time.Time // 上次刷新时间
}

func NewGeeRegistryDiscovery(addr string, timeout time.Duration) *GeeRegistryDiscovery {
	if timeout == 0 {
		timeout = time.Second * 10
	}
	d := &GeeRegistryDiscovery{
		MultiServerDiscovery: NewMultiServerDiscovery(make([]string, 0)),
		registry: addr,
		timeout: timeout,
	}
	return d
}

func (d *GeeRegistryDiscovery) Update(servers []string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.servers = servers
	d.lastUpdate = time.Now()
	return nil
}

func (d *GeeRegistryDiscovery) Refresh() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.lastUpdate.Add(d.timeout).After(time.Now()) {
		return nil
	}

	log.Println("rpc registry: refresh servers from registry", d.registry)
	resp, err := http.Get(d.registry)
	if err != nil {
		log.Println("rpc registry refresh err:", err)
		return err
	}
	servers := strings.Split(resp.Header.Get("x-geerpc-server"), ",")
	d.servers = make([]string, 0, len(servers))
	for _, server := range servers {
		if strings.TrimSpace(server) != "" {
			d.servers = append(d.servers, strings.TrimSpace(server))
		}
	}
	d.lastUpdate = time.Now()
	return nil
}

func (d *GeeRegistryDiscovery)Get(mode SelectMode) (string, error) {
	if err := d.Refresh(); err != nil {
		return "", err
	}
	return d.MultiServerDiscovery.Get(mode)
}

func (d *GeeRegistryDiscovery)GetAll() ([]string, error) {
	if err := d.Refresh(); err != nil {
		return nil, err
	}
	return d.MultiServerDiscovery.GetAll()
}