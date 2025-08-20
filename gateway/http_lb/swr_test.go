package httplb

import "testing"

func TestSSRB(t *testing.T) {
	lb := NewSmoothWeightedRoundRobin(map[string]WeightInfo{

		"10.0.0.1": {
			Weight:  5,
			Timeout: 100,
		},
		"10.0.0.2": {
			Weight:  1,
			Timeout: 100,
		},
	})

	// 初始选取
	for i := 0; i < 10; i++ {
		node := lb.Pick()
		println("Pick:", node.addr)
	}

	// 更新权重
	lb.UpdateWeight("10.0.0.2", 3) // 更新已有
	lb.UpdateWeight("10.0.0.1", 0) // 删除已有
	lb.UpdateWeight("10.0.0.3", 4) // 新增节点

	println("After update:")
	for i := 0; i < 10; i++ {
		node := lb.Pick()
		println("Pick:", node.addr)
	}
}
