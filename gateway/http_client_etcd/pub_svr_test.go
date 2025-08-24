package httpclientetcd

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"testing"

	"github.com/zeromicro/go-zero/core/discov"
	clientv3 "go.etcd.io/etcd/client/v3"
)

func TestPubServer(t *testing.T) {
	// etcd 连接配置
	etcdCfg := &discov.EtcdConf{
		Hosts: []string{"127.0.0.1:2379"},
		Key:   "rpc_etcd1.rpc.1",
	}
	InitEtcConfig(etcdCfg)

	cli, err := clientv3.New(*GetEtcdConfig(etcdCfg))
	if err != nil {
		log.Fatalf("failed to connect etcd: %v", err)
	}
	defer cli.Close()

	// 服务端参数：端口可通过环境变量 PORT 指定
	port := 9081
	if p := os.Getenv("PORT"); p != "" {
		fmt.Sscanf(p, "%d", &port)
	}

	// 向etcd 上报的数据内容
	svc := ServiceInstance{
		Name: etcdCfg.Key,
		// ID:   serviceID,
		Addr: fmt.Sprintf("127.0.0.1:%d", port),
	}

	// 创建 Registrar 并注册服务
	registrar, err := NewRegistrar(cli, svc, 5)
	if err != nil {
		log.Fatalf("failed to create registrar: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := registrar.Register(ctx); err != nil {
		log.Fatalf("register service failed: %v", err)
	}
	log.Printf("service registered: %s -> %s", svc.ID, svc.Addr)

	// HTTP 服务处理
	http.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "hello from %s", svc.Addr)
	})

	// 优雅关闭信号
	go func() {
		ch := make(chan os.Signal, 1)
		// 监听中断信号
		signal.Notify(ch, os.Interrupt, os.Kill)
		<-ch
		log.Println("shutting down server")
		_ = registrar.Deregister(ctx)
		cancel()
		os.Exit(0)
	}()

	// 启动 HTTP 服务
	log.Printf("HTTP server listening on %s", fmt.Sprintf("127.0.0.1:%d", port))
	if err := http.ListenAndServe(fmt.Sprintf(":%d", port), nil); err != nil {
		log.Fatalf("server exited with error: %v", err)
	}
}
