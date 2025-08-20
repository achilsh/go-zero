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
	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/rest/httpc"
	"github.com/zeromicro/go-zero/rest/httpx"
)

type HttpLBNode struct {
	LbHandle *httplb.SmoothWeightedRoundRobin
	Name     string
}

type HttpLBManager struct {
	ServerLbSet map[string]HttpLBNode //多个服务的路由列表;key为服务名
}

func (hlbm *HttpLBManager) Init(upstreamCfg []Upstream) {

	for _, upstream := range upstreamCfg {
		item := HttpLBNode{
			Name: upstream.Name,
		}
		if upstream.Http == nil {
			continue
		}

		if len(upstream.Http.NodeList) <= 0 {
			continue
		}

		// 初始化服务名下的一组后端路由节点
		var lbTemp = make(map[string]httplb.WeightInfo)
		for _, lb := range upstream.Http.NodeList {
			lbTemp[lb.Target] = httplb.WeightInfo{
				Weight:  lb.Weight,
				Timeout: int(lb.Timeout),
			}
		}
		item.LbHandle = httplb.NewSmoothWeightedRoundRobin(lbTemp)
		hlbm.ServerLbSet[upstream.Name] = item
	}
}

func (hlbm *HttpLBManager) FindLbServerListByServerName(serverName string) *httplb.SmoothWeightedRoundRobin {
	if hlbm == nil || len(serverName) <= 0 {
		return nil
	}
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
		ServerLbSet: make(map[string]HttpLBNode),
	}
)

func (s *Server) buildHttpRoute_LB(up Upstream, writer mr.Writer[rest.Route]) {
	lbNodeList := HttpLBItems.FindLbServerListByServerName(up.Name)
	if lbNodeList == nil {
		panic(fmt.Sprintf("not find any lb node list for: %v", up.Name))
	}
	for _, m := range up.Mappings {
		writer.Write(rest.Route{
			Method:  strings.ToUpper(m.Method),
			Path:    m.Path,
			Handler: s.buildHttpHandler_LB(lbNodeList, up.Name),
		})
	}
}

func (s *Server) buildHttpHandler_LB(lbList *httplb.SmoothWeightedRoundRobin, serverName string) http.HandlerFunc {
	handler := func(w http.ResponseWriter, r *http.Request) {
		if lbList == nil {
			panic(fmt.Sprintf("not get lb node for this server: %v", serverName))
		}
		target := lbList.Pick()
		if target == nil {
			fmt.Println("has no any for upstream router.")
			return
		}

		w.Header().Set(httpx.ContentType, httpx.JsonContentType)
		req, err := buildRequestWithNewTarget_LB(r, target.GetAddr())
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

func buildRequestWithNewTarget_LB(r *http.Request, target string) (*http.Request, error) {
	u := *r.URL
	u.Host = target
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
