package gateway

type RewriteRuleCfgItem struct {
	Method   string       `json:"Method,optional"` // 忽略大小写
	Pattern  string       `json:"Pattern"`         // 匹配外面请求的模式；可以是精准字符串，也可以是正则表达式
	Target   string       `json:"Target"`          //目标url
	NodeList []NodeWeight `json:"NodeList,optional"`
}
