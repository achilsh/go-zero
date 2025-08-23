package httplb

import (
	"sync"
)

type SmoothWeightedNode struct {
	weight          int
	currentWeight   int
	effectiveWeight int
	addr            string
	timeout         int //毫秒
}

func (s *SmoothWeightedNode) GetTimeout() int {
	return s.timeout
}
func (s *SmoothWeightedNode) GetAddr() string {
	return s.addr
}

type SmoothWeightedRoundRobin struct {
	nodes []*SmoothWeightedNode
	lock  sync.RWMutex
}

type WeightInfo struct {
	Weight  int
	Timeout int
}

// NewSmoothWeightedRoundRobin 创建实例; 入参为: ip/port地址-权重 映射关系, 毫秒
func NewSmoothWeightedRoundRobin(nodes map[string]WeightInfo) *SmoothWeightedRoundRobin {
	var s *SmoothWeightedRoundRobin = nil
	for addr, w := range nodes {
		if w.Weight > 0 {
			if s == nil {
				s = &SmoothWeightedRoundRobin{}
			}
			s.nodes = append(s.nodes, &SmoothWeightedNode{
				weight:          w.Weight,
				effectiveWeight: w.Weight,
				addr:            addr,
				timeout:         w.Timeout,
			})
		}
	}
	return s
}

// Pick 选择节点
func (s *SmoothWeightedRoundRobin) Pick() *SmoothWeightedNode {
	s.lock.Lock()
	defer s.lock.Unlock()

	var best *SmoothWeightedNode
	total := 0

	for _, node := range s.nodes {
		total += node.effectiveWeight
		node.currentWeight += node.effectiveWeight

		if best == nil || node.currentWeight > best.currentWeight {
			best = node
		}
	}

	if best == nil {
		return nil
	}

	best.currentWeight -= total
	return best
}

// UpdateWeight 动态更新节点权重
func (s *SmoothWeightedRoundRobin) UpdateWeight(addr string, weight int) {
	s.lock.Lock()
	defer s.lock.Unlock()

	// 查找是否已有该节点
	for i, node := range s.nodes {
		if node.addr == addr {
			if weight == 0 {
				// 删除节点
				s.nodes = append(s.nodes[:i], s.nodes[i+1:]...)
			} else {
				// 更新权重
				node.weight = weight
				node.effectiveWeight = weight
			}
			return
		}
	}

	// 新增节点
	if weight > 0 {
		s.nodes = append(s.nodes, &SmoothWeightedNode{
			weight:          weight,
			effectiveWeight: weight,
			addr:            addr,
		})
	}
}

// Nodes 返回当前节点信息（调试用）
func (s *SmoothWeightedRoundRobin) Nodes() []map[string]interface{} {
	s.lock.RLock()
	defer s.lock.RUnlock()
	result := make([]map[string]interface{}, 0, len(s.nodes))
	for _, n := range s.nodes {
		result = append(result, map[string]interface{}{
			"addr":            n.addr,
			"weight":          n.weight,
			"effectiveWeight": n.effectiveWeight,
			"currentWeight":   n.currentWeight,
		})
	}
	return result
}
