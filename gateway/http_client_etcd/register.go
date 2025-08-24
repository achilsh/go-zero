package httpclientetcd

import (
	"context"
	"fmt"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

const (
	// Delimiter is a separator that separates the etcd path.
	Delimiter = '/'

	autoSyncInterval   = time.Minute
	coolDownInterval   = time.Second
	dialTimeout        = 5 * time.Second
	requestTimeout     = 3 * time.Second
	endpointsSeparator = ","
)

type Registrar struct {
	cli     *clientv3.Client
	key     string
	value   string
	leaseID clientv3.LeaseID
	ttl     int64
	fullKey string
	id      int64
}

func makeEtcdKey(key string, id int64) string {
	return fmt.Sprintf("%s%c%d", key, Delimiter, id)
}

func NewRegistrar(cli *clientv3.Client, svc ServiceInstance, ttlSeconds int64) (*Registrar, error) {
	if ttlSeconds <= 0 {
		ttlSeconds = 10
	}
	// b, _ := json.Marshal(svc)
	b := svc.Addr
	key := fmt.Sprintf("%s", svc.Name)
	return &Registrar{
		cli:   cli,
		key:   key,
		value: string(b),
		ttl:   ttlSeconds,
	}, nil
}

func (r *Registrar) Register(ctx context.Context) error {
	lease, err := r.cli.Grant(ctx, r.ttl)
	if err != nil {
		return err
	}
	r.leaseID = lease.ID
	if r.id > 0 {
		r.fullKey = makeEtcdKey(r.key, r.id)
	} else {
		r.fullKey = makeEtcdKey(r.key, int64(r.leaseID))
	}

	if _, err = r.cli.Put(ctx, r.fullKey, r.value, clientv3.WithLease(lease.ID)); err != nil {
		return err
	}

	ch, err := r.cli.KeepAlive(ctx, r.leaseID)
	if err != nil {
		return err
	}

	go func() {
		for {
			select {
			case <-ch:
			// keepalive response
			case <-ctx.Done():
				return
			}
		}
	}()
	return nil
}

func (r *Registrar) Deregister(ctx context.Context) error {
	if r.leaseID != 0 {
		_, err := r.cli.Revoke(ctx, r.leaseID)
		return err
	}
	_, err := r.cli.Delete(ctx, r.key)
	return err
}
