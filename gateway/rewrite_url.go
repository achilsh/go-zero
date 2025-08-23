package gateway

import (
	httprewrite "github.com/zeromicro/go-zero/gateway/http_rewrite"
)

// 所有服务下的 重写url 句柄
var RewriteURLHandler *URLRewriteManager = &URLRewriteManager{
	// 每个服务对应的重写规则
	RewriteUrlSet: make(map[string]*UrlRewriteNode),
}

type URLRewriteManager struct {
	RewriteUrlSet map[string]*UrlRewriteNode //RewriteKeyType.Key()
}

type UrlRewriteNode struct {
	Name        string //服务名字
	RewriteNode RewriteKeyType
	RewriteR    *httprewrite.RegexpRouter
}

func (u *URLRewriteManager) FindUrlRewriteHandle(rewriteNode RewriteKeyType) *UrlRewriteNode {
	serverName := rewriteNode.Key()
	if u == nil || len(serverName) <= 0 {
		return nil
	}

	if _, ok := u.RewriteUrlSet[serverName]; !ok {
		return nil
	}
	return u.RewriteUrlSet[serverName]
}

// 根据rewrite 配置 来 构建 rewrite handle.
func (u *URLRewriteManager) Init(ups []Upstream) {
	// 遍历每个服务的配置
	for _, up := range ups {
		var rewriteKey RewriteKeyType

		if len(up.Name) <= 0 {
			continue
		}
		rewriteKey.UpstreamName = up.Name

		for _, mapping := range up.Mappings {
			if mapping.HttpProxy != 1 {
				continue
			}
			rewriteKey.MappingPath = mapping.Path

			for _, rewrite := range mapping.Rewrites {
				rewriteKey.RewritePattern = rewrite.Pattern
				rewriteKey.RewriteTarget = rewrite.Target

				item := UrlRewriteNode{
					Name:        up.Name,
					RewriteNode: rewriteKey,
					RewriteR:    httprewrite.NewRegexpRouter(),
				}

				item.RewriteR.AddRule(rewrite.Method, rewrite.Pattern, rewrite.Target, 0)
				u.RewriteUrlSet[rewriteKey.Key()] = &item
			}
		}
	}
}
