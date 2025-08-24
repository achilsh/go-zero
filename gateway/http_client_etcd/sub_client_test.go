package httpclientetcd

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"

	"github.com/zeromicro/go-zero/core/discov"
	clientv3 "go.etcd.io/etcd/client/v3"
)

func TestClientSub(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// etcd 连接配置
	etcdCfg := &discov.EtcdConf{
		Hosts: []string{"127.0.0.1:2379"},
		Key:   "rpc_etcd1.rpc.1",
	}
	InitEtcConfig(etcdCfg)

	// 连接 etcd
	cli, err := clientv3.New(*GetEtcdConfig(etcdCfg))
	if err != nil {
		log.Fatalf("failed to connect etcd: %v", err)
	}
	defer cli.Close()

	// 初始化 Discovery，订阅 "demo-service"
	discovery := NewDiscovery(cli, etcdCfg.Key)
	if err := discovery.Start(ctx); err != nil {
		log.Fatalf("discovery start error: %v", err)
	}

	// 打印本地快照（模拟订阅行为）
	go func() {
		for {
			select {
			case <-time.After(3 * time.Second):
				instances := discovery.Snapshot()
				fmt.Println("Current instances:")
				for _, inst := range instances {
					fmt.Printf("Addr=%s\n", inst)
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	//业务使用方，即客户端 要使用的化，就是直接使用  discovery.Snapshot() 中的数据即可。

	// 优雅退出
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	fmt.Println("shutting down client")
}
