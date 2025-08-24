package httpclientetcd

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/zeromicro/go-zero/core/discov"
	clientv3 "go.etcd.io/etcd/client/v3"
)

var (
	accounts   = make(map[string]Account)
	tlsConfigs = make(map[string]*tls.Config)
	lock       sync.RWMutex

	// DialTimeout is the dial timeout.
	DialTimeout = dialTimeout
)

// Account holds the username/password for an etcd cluster.
type Account struct {
	User string
	Pass string
}

func getClusterKey(endpoints []string) string {
	sort.Strings(endpoints)
	return strings.Join(endpoints, endpointsSeparator)
}

func makeKeyPrefix(key string) string {
	return fmt.Sprintf("%s%c", key, Delimiter)
}

// AddAccount adds the username/password for the given etcd cluster.
func AddAccount(endpoints []string, user, pass string) {
	lock.Lock()
	defer lock.Unlock()

	accounts[getClusterKey(endpoints)] = Account{
		User: user,
		Pass: pass,
	}
}

func RegisterAccount(endpoints []string, user, pass string) {
	AddAccount(endpoints, user, pass)
}

// RegisterTLS registers the CertFile/CertKeyFile/CACertFile to the given etcd.
func RegisterTLS(endpoints []string, certFile, certKeyFile, caFile string,
	insecureSkipVerify bool) error {
	return AddTLS(endpoints, certFile, certKeyFile, caFile, insecureSkipVerify)
}

// AddTLS adds the tls cert files for the given etcd cluster.
func AddTLS(endpoints []string, certFile, certKeyFile, caFile string, insecureSkipVerify bool) error {
	cert, err := tls.LoadX509KeyPair(certFile, certKeyFile)
	if err != nil {
		return err
	}

	caData, err := os.ReadFile(caFile)
	if err != nil {
		return err
	}

	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(caData)

	lock.Lock()
	defer lock.Unlock()
	tlsConfigs[getClusterKey(endpoints)] = &tls.Config{
		Certificates:       []tls.Certificate{cert},
		RootCAs:            pool,
		InsecureSkipVerify: insecureSkipVerify,
	}

	return nil
}

func GetAccount(endpoints []string) (Account, bool) {
	lock.RLock()
	defer lock.RUnlock()

	account, ok := accounts[getClusterKey(endpoints)]
	return account, ok
}

// GetTLS gets the tls config for the given etcd cluster.
func GetTLS(endpoints []string) (*tls.Config, bool) {
	lock.RLock()
	defer lock.RUnlock()

	cfg, ok := tlsConfigs[getClusterKey(endpoints)]
	return cfg, ok
}

func InitEtcConfig(conf *discov.EtcdConf) {
	if conf == nil {
		return
	}

	if err := conf.Validate(); err != nil {
		return
	}

	if conf.HasAccount() {
		RegisterAccount(conf.Hosts, conf.User, conf.Pass)
	}

	if conf.HasTLS() {
		if err := RegisterTLS(conf.Hosts, conf.CertFile, conf.CertKeyFile,
			conf.CACertFile, conf.InsecureSkipVerify); err != nil {
			return
		}
	}
}

func GetEtcdConfig(etcdCfg *discov.EtcdConf) *clientv3.Config {
	if etcdCfg == nil {
		return nil
	}
	cfg := clientv3.Config{
		Endpoints:           etcdCfg.Hosts,
		AutoSyncInterval:    autoSyncInterval,
		DialTimeout:         DialTimeout,
		RejectOldCluster:    true,
		PermitWithoutStream: true,
	}
	if account, ok := GetAccount(etcdCfg.Hosts); ok {
		cfg.Username = account.User
		cfg.Password = account.Pass
	}
	if tlsCfg, ok := GetTLS(etcdCfg.Hosts); ok {
		cfg.TLS = tlsCfg
	}
	return &cfg
}
