package gateway

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/zeromicro/go-zero/core/logc"
	"github.com/zeromicro/go-zero/core/mr"
	httplb "github.com/zeromicro/go-zero/gateway/http_lb"
	httprewrite "github.com/zeromicro/go-zero/gateway/http_rewrite"
	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/rest/httpc"
	"github.com/zeromicro/go-zero/rest/httpx"
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
	LbHandle    *httplb.SmoothWeightedRoundRobin //每个服务名下面的节点列表
	RewriteNode RewriteKeyType
}

type HttpLBManager struct {
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

				// 一个url 对应一组 lb 节点
				var lbTemp = make(map[string]httplb.WeightInfo)
				for _, lb := range rewrite.NodeList {
					lbTemp[lb.Node] = httplb.WeightInfo{
						Weight:  lb.Weight,
						Timeout: int(lb.Timeout),
					}
				}
				if len(lbTemp) <= 0 {
					continue
				}

				lbNodeTmp := httplb.NewSmoothWeightedRoundRobin(lbTemp)
				if lbNodeTmp == nil {
					continue
				}
				item := HttpLBNode{
					LbHandle:    lbNodeTmp,
					RewriteNode: rewriteKey,
				}
				hlbm.ServerLbSet[item.RewriteNode.Key()] = &item
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

	handler := func(w http.ResponseWriter, r *http.Request) {

		var lbList *httplb.SmoothWeightedRoundRobin = nil
		var dstRewriteUrl string

		for _, rewriteNodeValue := range rewwriteKeyNodeMap {

			targetPath := rewriteNodeValue.regRotuer.RewriteUrl(r.Method, r.URL.Path)
			if targetPath != rewriteNodeValue.rewriteNode.RewriteTarget {
				continue
			}

			lbList = HttpLBItems.FindLbServerListByServerName(rewriteNodeValue.rewriteNode)
			if lbList == nil {
				panic(fmt.Sprintf("not find any lb node list for: %v", rewriteNodeValue.rewriteNode.Key()))
			}
			dstRewriteUrl = targetPath
			break
		}
		// for _, rewrite := range mapping.Rewrites {
		// 	var rewriteNode RewriteKeyType = RewriteKeyType{
		// 		UpstreamName: upstreamName,
		// 		MappingPath:  mapping.Path,
		// 		//
		// 		RewritePattern: rewrite.Pattern,
		// 		RewriteTarget:  rewrite.Target,
		// 	}

		// 	rewriteNodeHandle := RewriteURLHandler.FindUrlRewriteHandle(rewriteNode)
		// 	if rewriteNodeHandle == nil {
		// 		continue
		// 	}

		// 	if rewriteNodeHandle.RewriteR == nil {
		// 		continue
		// 	}
		// 	targetPath := rewriteNodeHandle.RewriteR.RewriteUrl(r.Method, r.URL.Path)
		// 	if targetPath != rewrite.Target {
		// 		continue
		// 	}

		// 	lbList = HttpLBItems.FindLbServerListByServerName(rewriteNode)
		// 	if lbList == nil {
		// 		panic(fmt.Sprintf("not find any lb node list for: %v", rewriteNode.Key()))
		// 	}
		// 	dstRewriteUrl = targetPath
		// 	break
		// }

		if lbList == nil {
			panic(fmt.Sprintf("not get lb node for this server:"))
		}
		target := lbList.Pick()
		if target == nil {
			fmt.Println("has no any for upstream router.")
			return
		}

		logc.Infof(r.Context(), "========> lb origin path: %v, rewrite path: %v, target addr: %v",
			r.URL.Path, dstRewriteUrl, target)

		w.Header().Set(httpx.ContentType, httpx.JsonContentType)
		req, err := buildRequestWithNewTarget_LB(r, dstRewriteUrl, target.GetAddr())
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		// set the timeout if it's configured, take effect only if it's greater than 0
		// and less than the deadline of the original request
		if target.GetTimeout() > 0 {
			timeout := time.Duration(target.GetTimeout()) * time.Millisecond
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
