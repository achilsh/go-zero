package httpclientetcd

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
)

type Discovery struct {
	cli     *clientv3.Client
	service string
	prefix  string

	mu sync.RWMutex
	// cache map[string]ServiceInstance // 增量维护快照
	cache map[string]string // 增量维护快照，key 是 rpc_etcd1.rpc.1/7668672329854348691
	rev   int64

	updates chan clientv3.WatchResponse
}

func (d *Discovery) GetKey() string {
	return d.service
}

func NewDiscovery(cli *clientv3.Client, service string) *Discovery {
	if !strings.HasPrefix(service, "/") {
		service = "/" + service
	}
	service = strings.TrimPrefix(service, "/")
	return &Discovery{
		cli:     cli,
		service: service,
		prefix:  fmt.Sprintf("%s/", service),
	}
}

func (d *Discovery) Start(ctx context.Context) error {
	d.cache = make(map[string]string)

	resp, err := d.cli.Get(ctx, d.prefix, clientv3.WithPrefix())
	if err != nil {
		return err
	}
	for _, kv := range resp.Kvs {
		// var si ServiceInstance
		if len(kv.Value) <= 0 {
			continue
		}

		fmt.Printf("discovery router cache key: %v\n", string(kv.Key))
		//输出的格式是： rpc_etcd1.rpc.1/7668672329854348691，=> key:lease
		d.cache[string(kv.Key)] = string(kv.Value)
		// if err := json.Unmarshal(kv.Value, &si); err == nil {
		// 	d.cache[string(kv.Key)] = si
		// }
	}
	d.rev = resp.Header.Revision

	d.updates = make(chan clientv3.WatchResponse, 100)
	go d.watch(ctx)
	go d.applyLoop(ctx)
	return nil
}

func (d *Discovery) watch(ctx context.Context) {
	rch := d.cli.Watch(ctx, d.prefix,
		clientv3.WithPrefix(),
		clientv3.WithRev(d.rev+1),
		clientv3.WithPrevKV(),
	)
	for wr := range rch {
		if wr.Err() != nil {
			fmt.Println("watch error:", wr.Err())
			time.Sleep(time.Second)
			continue
		}
		select {
		case d.updates <- wr:
		default:
			fmt.Println("updates channel full, dropping")
		}
	}
}

func (d *Discovery) applyLoop(ctx context.Context) {
	ticker := time.NewTicker(50 * time.Millisecond) // 去抖
	defer ticker.Stop()

	var pending []clientv3.WatchResponse

	for {
		select {
		case wr := <-d.updates:
			pending = append(pending, wr)
		case <-ticker.C:
			if len(pending) > 0 {
				d.applyEvents(pending)
				pending = nil
			}
		case <-ctx.Done():
			return
		}
	}
}

func (d *Discovery) applyEvents(events []clientv3.WatchResponse) {
	d.mu.Lock()
	defer d.mu.Unlock()

	for _, wr := range events {
		for _, ev := range wr.Events {
			if ev.Kv.ModRevision <= d.rev {
				continue
			}
			d.rev = ev.Kv.ModRevision

			switch ev.Type {
			case mvccpb.PUT:
				// var si ServiceInstance
				if len(ev.Kv.Value) <= 0 {
					continue
				}
				d.cache[string(ev.Kv.Key)] = string(ev.Kv.Value)
				// if err := json.Unmarshal(ev.Kv.Value, &si); err == nil {
				// 	d.cache[string(ev.Kv.Key)] = si
				// }
			case mvccpb.DELETE:
				delete(d.cache, string(ev.Kv.Key))
			}
		}
	}
}

func (d *Discovery) Snapshot() []string {
	d.mu.RLock()
	defer d.mu.RUnlock()

	// out := make([]ServiceInstance, 0, len(d.cache))
	out := make([]string, 0, len(d.cache))
	for _, v := range d.cache {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// key 格式是：rpc_etcd1.rpc.1/7668672329854348691
func (d *Discovery) SnapshotMap() map[string]string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	ret := make(map[string]string)
	for k, v := range d.cache {
		ret[k] = v
	}
	return ret
}
