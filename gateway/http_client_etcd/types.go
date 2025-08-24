package httpclientetcd

type ServiceInstance struct {
	Name string            `json:"name,omitempty"`
	ID   string            `json:"id,omitempty"`
	Addr string            `json:"addr"`
	Meta map[string]string `json:"meta,omitempty"`
}
