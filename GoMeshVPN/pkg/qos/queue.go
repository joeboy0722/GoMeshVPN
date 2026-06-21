package qos

import (
	"container/list"
	"sync"
)

// Packet 封裝的封包（包含優先級資訊）
type Packet struct {
	Data     []byte
	Priority Priority
}

// PriorityQueue 優先級佇列（三級）
type PriorityQueue struct {
	highQueue   *list.List
	mediumQueue *list.List
	lowQueue    *list.List

	mu sync.Mutex

	// 統計資訊
	highCount   int
	mediumCount int
	lowCount    int

	// 調度權重（防止低優先級飢餓）
	highWeight   int // 高優先級權重
	mediumWeight int // 中優先級權重
	lowWeight    int // 低優先級權重

	roundRobinCounter int // 輪詢計數器

	// 新增：容量控制與信號通知
	capacity   int
	signalChan chan struct{}
}

// NewPriorityQueue 建立優先級佇列
func NewPriorityQueue(capacity int) *PriorityQueue {
	return &PriorityQueue{
		highQueue:   list.New(),
		mediumQueue: list.New(),
		lowQueue:    list.New(),

		// 預設權重：高:中:低 = 70:20:10
		highWeight:   70,
		mediumWeight: 20,
		lowWeight:    10,

		capacity:   capacity,
		signalChan: make(chan struct{}, 1),
	}
}

// Enqueue 將封包加入對應的優先級佇列
// 返回 true 表示成功，false 表示佇列已滿
func (pq *PriorityQueue) Enqueue(pkt Packet) bool {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	currentSize := pq.highCount + pq.mediumCount + pq.lowCount
	if currentSize >= pq.capacity {
		return false // 佇列已滿，丟棄封包
	}

	switch pkt.Priority {
	case PriorityHigh:
		pq.highQueue.PushBack(pkt)
		pq.highCount++
	case PriorityMedium:
		pq.mediumQueue.PushBack(pkt)
		pq.mediumCount++
	case PriorityLow:
		pq.lowQueue.PushBack(pkt)
		pq.lowCount++
	}

	// 非阻塞發送通知信號
	select {
	case pq.signalChan <- struct{}{}:
	default:
	}

	return true
}

// Dequeue 從佇列中取出下一個要發送的封包（加權輪詢）
func (pq *PriorityQueue) Dequeue() (Packet, bool) {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	// 使用加權輪詢避免飢餓
	totalWeight := pq.highWeight + pq.mediumWeight + pq.lowWeight
	if totalWeight == 0 {
		return Packet{}, false
	}
	pq.roundRobinCounter = (pq.roundRobinCounter + 1) % totalWeight

	// 決定從哪個佇列取封包
	if pq.roundRobinCounter < pq.highWeight {
		if elem := pq.highQueue.Front(); elem != nil {
			pkt := pq.highQueue.Remove(elem).(Packet)
			pq.highCount--
			return pkt, true
		}
	} else if pq.roundRobinCounter < pq.highWeight+pq.mediumWeight {
		if elem := pq.mediumQueue.Front(); elem != nil {
			pkt := pq.mediumQueue.Remove(elem).(Packet)
			pq.mediumCount--
			return pkt, true
		}
	} else {
		if elem := pq.lowQueue.Front(); elem != nil {
			pkt := pq.lowQueue.Remove(elem).(Packet)
			pq.lowCount--
			return pkt, true
		}
	}

	// Fallback mechanism if targeted queue is empty
	if elem := pq.highQueue.Front(); elem != nil {
		pkt := pq.highQueue.Remove(elem).(Packet)
		pq.highCount--
		return pkt, true
	}

	if elem := pq.mediumQueue.Front(); elem != nil {
		pkt := pq.mediumQueue.Remove(elem).(Packet)
		pq.mediumCount--
		return pkt, true
	}

	if elem := pq.lowQueue.Front(); elem != nil {
		pkt := pq.lowQueue.Remove(elem).(Packet)
		pq.lowCount--
		return pkt, true
	}

	return Packet{}, false
}

// Len 返回佇列中的總封包數
func (pq *PriorityQueue) Len() int {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	return pq.highCount + pq.mediumCount + pq.lowCount
}

// Signal 返回通知通道
func (pq *PriorityQueue) Signal() <-chan struct{} {
	return pq.signalChan
}

// GetStats 取得佇列統計資訊（用於監控）
func (pq *PriorityQueue) GetStats() (high, medium, low int) {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	return pq.highCount, pq.mediumCount, pq.lowCount
}

// SetWeights 設定調度權重（允許動態調整）
func (pq *PriorityQueue) SetWeights(high, medium, low int) {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	pq.highWeight = high
	pq.mediumWeight = medium
	pq.lowWeight = low
}
