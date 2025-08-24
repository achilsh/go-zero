package gateway

import (
	"context"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/zeromicro/go-zero/core/logc"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/mr"
	etcdlb "github.com/zeromicro/go-zero/gateway/http_client_etcd"
	httplb "github.com/zeromicro/go-zero/gateway/http_lb"
	httprewrite "github.com/zeromicro/go-zero/gateway/http_rewrite"
	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/rest/httpc"
	"github.com/zeromicro/go-zero/rest/httpx"
	clientv3 "go.etcd.io/etcd/client/v3"
)

type RewriteKeyType struct {
	UpstreamName string
	MappingPath  string
	//
	RewritePattern string
	RewriteTarget  string
}

func (u RewriteKeyType) Key() string {
	s := fmt.Sprintf("%s:%s:%s:%s", u.UpstreamName, u.MappingPath, u.RewritePattern, u.RewriteTarget)
	return s
}

type HttpLBNode struct {
	LbHandle     *httplb.SmoothWeightedRoundRobin //每条rewrite对应的负载均衡器的节点列表
	RewriteNode  RewriteKeyType                   //每条rewrite规则
	EtcdDiscover *etcdlb.Discovery                //如果 NodeList 为空，则通过 etcd 获取节点
}

type HttpLBManager struct {
	// 一条rewrite规则 对应 一个负载均衡节点， 对应下面map中的一条记录。
	//如果配置中的 某条记录 NodeList 为空，该map 就不会有这条记录对应的负载均衡器记录，比如 RewriteKeyType.Key() 该条记录对应的value为空
	ServerLbSet map[string]*HttpLBNode //多个服务的路由列表;key 为 RewriteKeyType.Key()

}

func (hlbm *HttpLBManager) Init(upstreamCfg []Upstream) {
	for _, upstream := range upstreamCfg {
		var rewriteKey RewriteKeyType

		if len(upstream.Name) <= 0 {
			continue
		}
		rewriteKey.UpstreamName = upstream.Name

		for _, mapping := range upstream.Mappings {
			if len(mapping.Path) <= 0 {
				continue
			}
			rewriteKey.MappingPath = mapping.Path

			for _, rewrite := range mapping.Rewrites {
				if len(rewrite.Pattern) <= 0 || len(rewrite.Target) <= 0 {
					continue
				}
				rewriteKey.RewritePattern = rewrite.Pattern
				rewriteKey.RewriteTarget = rewrite.Target

				// 一个url 对应一组 lb 节点；如果 NodeList 为空，则 hlbm.ServerLbSet 对应的该条的负载均衡器记录不存在
				if len(rewrite.NodeList) > 0 {
					var lbTemp = make(map[string]httplb.WeightInfo)
					for _, lb := range rewrite.NodeList {
						lbTemp[lb.Node] = httplb.WeightInfo{
							Weight:  lb.Weight,
							Timeout: int(lb.Timeout),
						}
					}

					if len(lbTemp) > 0 {
						lbNodeTmp := httplb.NewSmoothWeightedRoundRobin(lbTemp)
						if lbNodeTmp != nil {
							if _, ok := hlbm.ServerLbSet[rewriteKey.Key()]; ok {
								hlbm.ServerLbSet[rewriteKey.Key()].LbHandle = lbNodeTmp
								hlbm.ServerLbSet[rewriteKey.Key()].RewriteNode = rewriteKey
							} else {
								hlbm.ServerLbSet[rewriteKey.Key()] = &HttpLBNode{
									LbHandle:    lbNodeTmp,
									RewriteNode: rewriteKey,
								}
							}
						}
					}
				}

				if rewrite.Etcd != nil {
					// etcd 连接配置
					etcdlb.InitEtcConfig(rewrite.Etcd)

					// 连接 etcd
					cli, err := clientv3.New(*etcdlb.GetEtcdConfig(rewrite.Etcd))
					if err != nil {
						log.Fatalf("failed to connect etcd: %v", err)
						continue
					}

					// 初始化 Discovery，订阅 "demo-service"
					discovery := etcdlb.NewDiscovery(cli, rewrite.Etcd.Key)
					if err := discovery.Start(context.Background()); err != nil {
						log.Fatalf(
							"discovery start error: %v, key: %v, host: %v",
							err,
							rewrite.Etcd.Key,
							rewrite.Etcd.Hosts,
						)
						cli.Close()
						continue
					} else {

						if _, ok := hlbm.ServerLbSet[rewriteKey.Key()]; ok {
							hlbm.ServerLbSet[rewriteKey.Key()].EtcdDiscover = discovery
							hlbm.ServerLbSet[rewriteKey.Key()].RewriteNode = rewriteKey
						} else {
							hlbm.ServerLbSet[rewriteKey.Key()] = &HttpLBNode{
								EtcdDiscover: discovery,
								RewriteNode:  rewriteKey,
							}
						}
					}
				}
			}
		}
	}
}

func (hlbm *HttpLBManager) FindLbServerListByServerName(rewriteNode RewriteKeyType) *httplb.SmoothWeightedRoundRobin {
	if hlbm == nil {
		return nil
	}
	serverName := rewriteNode.Key()
	if _, ok := hlbm.ServerLbSet[serverName]; !ok {
		return nil
	}

	lbNodeHandle := hlbm.ServerLbSet[serverName].LbHandle
	if lbNodeHandle == nil {
		return nil
	}

	return lbNodeHandle
}

func (hlbm *HttpLBManager) FindEtcdDiscoverByServerName(rewriteNode RewriteKeyType) *etcdlb.Discovery {
	if hlbm == nil {
		return nil
	}
	serverName := rewriteNode.Key()
	if _, ok := hlbm.ServerLbSet[serverName]; !ok {
		return nil
	}

	etcHandle := hlbm.ServerLbSet[serverName].EtcdDiscover
	if etcHandle == nil {
		return nil
	}

	return etcHandle
}

var (
	HttpLBItems *HttpLBManager = &HttpLBManager{
		ServerLbSet: make(map[string]*HttpLBNode),
	}
)

func (s *Server) buildHttpRoute_LB(up Upstream, writer mr.Writer[rest.Route]) {
	// 复用原有的逻辑，如果配置 Mappings 就按 原有的url规则接收数据
	for _, m := range up.Mappings {
		writer.Write(rest.Route{
			Method:  strings.ToUpper(m.Method),
			Path:    m.Path,
			Handler: s.buildHttpHandler_LB(up.Name, m),
		})
	}
}

type RewriteNodeValue struct {
	rewriteNode RewriteKeyType
	regRotuer   *httprewrite.RegexpRouter
}

func (s *Server) buildHttpHandler_LB(upstreamName string, mapping RouteMapping) http.HandlerFunc {
	var rewwriteKeyNodeMap map[string]RewriteNodeValue = make(map[string]RewriteNodeValue)
	//
	for _, rewrite := range mapping.Rewrites {
		var rewriteNode RewriteKeyType = RewriteKeyType{
			UpstreamName: upstreamName,
			MappingPath:  mapping.Path,
			//
			RewritePattern: rewrite.Pattern,
			RewriteTarget:  rewrite.Target,
		}

		rewriteNodeHandle := RewriteURLHandler.FindUrlRewriteHandle(rewriteNode)
		if rewriteNodeHandle == nil {
			continue
		}
		if rewriteNodeHandle.RewriteR == nil {
			continue
		}

		rewwriteKeyNodeMap[rewriteNode.Key()] = RewriteNodeValue{
			regRotuer:   rewriteNodeHandle.RewriteR,
			rewriteNode: rewriteNode,
		}
	}

	// 对每个请求的处理逻辑:
	handler := func(w http.ResponseWriter, r *http.Request) {

		var lbList *httplb.SmoothWeightedRoundRobin = nil
		var etcdHandle *etcdlb.Discovery = nil
		var dstRewriteUrl string
		var retwriteNodeFind *RewriteNodeValue = nil

		for _, rewriteNodeValueNode := range rewwriteKeyNodeMap {

			retwriteNodeFind = &rewriteNodeValueNode
			//根据实际请求的url找到对应的 重写规则记录，判断最终 target url是否是和配置中的
			targetPath := rewriteNodeValueNode.regRotuer.RewriteUrl(r.Method, r.URL.Path)
			if targetPath == "" {
				continue
			}
			if targetPath != rewriteNodeValueNode.rewriteNode.RewriteTarget {
				continue
			}

			dstRewriteUrl = targetPath
			//优先使用 配置的后端节点 负载均衡里列表
			lbList = HttpLBItems.FindLbServerListByServerName(rewriteNodeValueNode.rewriteNode)
			if lbList != nil {
				logx.Debugf("find lb direct node list for: %v", rewriteNodeValueNode.rewriteNode.Key())
				break
			}

			etcdHandle = HttpLBItems.FindEtcdDiscoverByServerName(rewriteNodeValueNode.rewriteNode)
			if etcdHandle != nil {
				logx.Debugf("find lb etcd node list for: %v", rewriteNodeValueNode.rewriteNode.Key())
				break
			}
		}

		if lbList == nil && etcdHandle == nil {
			httpx.ErrorCtx(
				r.Context(),
				w,
				fmt.Errorf("not find any avail node, key: %s", retwriteNodeFind.rewriteNode.Key()),
			)
			panic(fmt.Sprintf("not config lb node or etcd for this server: %v", upstreamName))
			return
		}

		var remoteAddr string
		var targetTmout int = 0
		//
		if lbList != nil {
			target := lbList.Pick()
			if target == nil {
				httpx.ErrorCtx(
					r.Context(),
					w,
					fmt.Errorf("from lb not get any node, key: %s", retwriteNodeFind.rewriteNode.Key()),
				)

				fmt.Println("has no any for upstream router, key: ", retwriteNodeFind.rewriteNode.Key())
				return
			}

			logc.Infof(r.Context(), "========> lb origin path: %v, rewrite path: %v, target addr: %v",
				r.URL.Path, dstRewriteUrl, target)

			remoteAddr = target.GetAddr()
			targetTmout = target.GetTimeout()
		}

		if etcdHandle != nil {
			keysAddrMap := etcdHandle.Snapshot()
			if len(keysAddrMap) == 0 {
				logx.Errorf("etcd get no any upstream node, key: %v")
				httpx.ErrorCtx(
					r.Context(),
					w,
					fmt.Errorf("etcd get no any upstream node, key: %v", etcdHandle.GetKey()),
				)
				return
			}
			//

			randomGet := func(ips []string) string {
				if len(ips) == 0 {
					return ""
				}
				idx := rand.Intn(len(ips)) // [0, len(ips))
				return ips[idx]
			}

			target := randomGet(keysAddrMap)
			logc.Infof(r.Context(), "========> etcd origin path: %v, rewrite path: %v, in etcd addr: %v",
				r.URL.Path, dstRewriteUrl, target)

			remoteAddr = target
			targetTmout = 3000
		}

		w.Header().Set(httpx.ContentType, httpx.JsonContentType)
		req, err := buildRequestWithNewTarget_LB(r, dstRewriteUrl, remoteAddr)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		// set the timeout if it's configured, take effect only if it's greater than 0
		// and less than the deadline of the original request
		if targetTmout > 0 {
			timeout := time.Duration(targetTmout) * time.Millisecond
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()
			req = req.WithContext(ctx)
		}

		resp, err := httpc.DoRequest(req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}
		defer resp.Body.Close()

		for key, values := range resp.Header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}

		w.WriteHeader(resp.StatusCode)
		if _, err = io.Copy(w, resp.Body); err != nil {
			// log the error with original request info
			logc.Error(r.Context(), err)
		}
	}
	return s.buildChainHandler(handler)
}

func buildRequestWithNewTarget_LB(r *http.Request, dstPath string, target string) (*http.Request, error) {
	u := *r.URL
	// 主要是修改 upstream 的 host and path
	u.Host = target
	u.Path = dstPath
	if len(u.Scheme) == 0 {
		u.Scheme = defaultHttpScheme
	}

	// if len(target.Prefix) > 0 {
	// 	var err error
	// 	u.Path, err = url.JoinPath(target.Prefix, u.Path)
	// 	if err != nil {
	// 		return nil, err
	// 	}
	// }

	newReq := &http.Request{
		Method:        r.Method,
		URL:           &u,
		Header:        r.Header.Clone(),
		Proto:         r.Proto,
		ProtoMajor:    r.ProtoMajor,
		ProtoMinor:    r.ProtoMinor,
		ContentLength: r.ContentLength,
		Body:          io.NopCloser(r.Body),
	}

	// make sure the context is passed to the new request
	return newReq.WithContext(r.Context()), nil
}
