package registry

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

type ServerItem struct {
	Addr string
	start time.Time
}

func (s *ServerItem)alilve(timeout time.Duration) bool {
	return s.start.Add(timeout).After(time.Now())
}

type GeeRegistry struct { // 跑一个http服务，支持心跳包新增/更新服务节点，获取服务节点
	timeout time.Duration
	servers map[string]*ServerItem
	mu sync.Mutex
}

const (
	defaultPath = "/_geerpc/registry"
	defaultTimeOut = time.Minute * 5
)

func NewGeeRegistry(timeout time.Duration) *GeeRegistry {
	return &GeeRegistry{
		timeout: timeout,
		servers: make(map[string]*ServerItem),
	}
}

var DefaultGeeRegistry = NewGeeRegistry(defaultTimeOut)

func HandleHTTP() {
	DefaultGeeRegistry.HandleHTTP(defaultPath)
}

// 注册服务
// 获取服务
// 更新服务

func (gr *GeeRegistry)putServer(addr string) {
	gr.mu.Lock()
	defer gr.mu.Unlock()
	s := gr.servers[addr]
	if s == nil {
		gr.servers[addr] = &ServerItem{Addr: addr, start: time.Now()}
	} else {
		s.start = time.Now()
	}
}

func (gr *GeeRegistry)aliveServers() []string {
	gr.mu.Lock()
	defer gr.mu.Unlock()
	alives := make([]string, 0)
	for addr, s := range gr.servers {
		if gr.timeout == 0 || s.alilve(gr.timeout) {
			alives = append(alives, addr)
		} else {
			delete(gr.servers, addr)
		}
	}
	return alives
}

func (gr *GeeRegistry)ServeHTTP(w http.ResponseWriter, r *http.Request)  {
	switch r.Method {
	case "GET":
		w.Header().Set("x-geerpc-server", strings.Join(gr.aliveServers(), ","))
	case "POST":
		addr := r.Header.Get("x-geerpc-server")
		if addr == "" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		gr.putServer(addr)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (gr *GeeRegistry)HandleHTTP(path string) {
	http.Handle(path, gr)
	log.Printf(fmt.Sprintf("registry path: %s", path))
}

func HeartBeat(registry, addr string, duration time.Duration) {
	if duration == 0 {
		duration = defaultTimeOut - time.Minute * time.Duration(1)
	}
	err := sendHeartBeat(registry, addr)
	go func() {
		t := time.NewTicker(duration)
		for err == nil {
			<- t.C
			err = sendHeartBeat(registry, addr)
		}
	}()
}

func sendHeartBeat(registry, addr string) error {
	log.Println(addr, "send heart beat to registry", registry)
	hc := &http.Client{}
	req, _ := http.NewRequest("POST", registry, nil)
	req.Header.Set("x-geerpc-server", addr)
	if _, err := hc.Do(req); err != nil {
		log.Println("rpc server: heart beat err:", err)
		return err
	}
	return nil
}

