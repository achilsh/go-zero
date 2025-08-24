package gateway

import "github.com/zeromicro/go-zero/core/discov"

type RewriteRuleCfgItem struct {
	Method   string       `json:"Method,optional"`   // 忽略大小写
	Pattern  string       `json:"Pattern"`           // 匹配外面请求的模式；可以是精准字符串，也可以是正则表达式
	Target   string       `json:"Target"`            //目标url
	NodeList []NodeWeight `json:"NodeList,optional"` //直接配置的 后端 IP的节点，包括权重，请求超时设置

	// http url 重写后通过 etcd 服务发现的 etcd 配置
	Etcd discov.EtcdConf `json:",optional"`
}
