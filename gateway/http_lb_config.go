package gateway

type NodeWeight struct {
	Target  string `json:"Target"`           //ip:port
	Weight  int    `json:"Weight,default=0"` //
	Timeout int64  `json:",default=3000"`    //毫秒
}
