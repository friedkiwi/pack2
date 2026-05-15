package ftcomp

import (
	"fmt"
)

const (
	modelSymbolCount      = 0x1b1
	internalNodeThreshold = 0x06c4
	maxNodes              = 870
	fastBits              = 9
	fastSize              = 1 << fastBits
)

type huffNode struct {
	weight uint16
	parent uint16
	child0 uint16
	child1 uint16
}

type huffTable struct {
	nodes     [maxNodes]huffNode
	fastSym   [fastSize]uint16
	fastNBits [fastSize]uint8
	root      uint16
}

func buildHuffTable(weights []uint16) (*huffTable, error) {
	if len(weights) != modelSymbolCount {
		return nil, fmt.Errorf("%w: invalid Huffman weight count", ErrInvalidData)
	}

	t := &huffTable{}
	queue := make([]uint16, 0, modelSymbolCount)
	var lastZero uint16
	oneCount := 0
	for sym, weight := range weights {
		id := uint16(sym * 4)
		t.nodes[sym].weight = weight
		t.nodes[sym].parent = 0
		t.nodes[sym].child0 = 0
		t.nodes[sym].child1 = 0
		if weight == 0 {
			lastZero = id
			continue
		}
		if weight == 1 {
			var displaced uint16
			if oneCount < len(queue) {
				displaced = queue[oneCount]
			}
			queue = append(queue, displaced)
			queue[oneCount] = id
			oneCount++
			continue
		}
		queue = append(queue, id)
	}
	if len(queue) == 0 {
		return nil, fmt.Errorf("%w: empty Huffman table", ErrInvalidData)
	}
	if len(queue) == 1 {
		t.nodes[lastZero/4].weight = 1
		queue = append(queue, lastZero)
	}

	sortQueueDOS(queue, oneCount, len(queue)-1, t)

	nextInternal := uint16(internalNodeThreshold)
	queueRead := 0
	remaining := len(queue)
	if len(queue) != 2 {
		for {
			remaining--

			left := queue[queueRead]
			right := queue[queueRead+1]
			searchStart := queueRead + 2
			queueRead++
			parentWeight := t.nodes[left/4].weight + t.nodes[right/4].weight

			insert := len(queue)
			for i := searchStart; i < len(queue); i++ {
				if t.nodes[queue[i]/4].weight >= parentWeight {
					insert = i
					break
				}
			}

			if int(nextInternal/4) >= len(t.nodes) {
				return nil, fmt.Errorf("%w: Huffman tree too large", ErrInvalidData)
			}
			parent := nextInternal
			copy(queue[queueRead:insert-1], queue[queueRead+1:insert])
			queue[insert-1] = parent
			nextInternal += 4

			p := &t.nodes[parent/4]
			p.weight = parentWeight
			p.parent = 0
			p.child0 = left
			p.child1 = right
			t.nodes[left/4].parent = parent
			t.nodes[right/4].parent = parent

			if remaining == 2 {
				break
			}
		}
	}

	left := queue[queueRead]
	right := queue[queueRead+1]
	if int(nextInternal/4) >= len(t.nodes) {
		return nil, fmt.Errorf("%w: Huffman tree too large", ErrInvalidData)
	}
	t.root = nextInternal
	p := &t.nodes[t.root/4]
	p.weight = t.nodes[left/4].weight + t.nodes[right/4].weight
	p.parent = 0
	p.child0 = left
	p.child1 = right
	t.nodes[left/4].parent = t.root
	t.nodes[right/4].parent = t.root

	for prefix := 0; prefix < fastSize; prefix++ {
		bits := uint16(prefix << 7)
		nbits := uint8(0)
		nodeID := t.root
		for nodeID >= internalNodeThreshold && nbits < fastBits {
			if bits&0x8000 != 0 {
				nodeID = t.nodes[nodeID/4].child0
			} else {
				nodeID = t.nodes[nodeID/4].child1
			}
			bits <<= 1
			nbits++
		}
		t.fastSym[prefix] = nodeID
		t.fastNBits[prefix] = nbits
	}

	return t, nil
}

func sortQueueDOS(queue []uint16, low, high int, t *huffTable) {
	if low >= high {
		return
	}

	type span struct {
		low  int
		high int
	}
	stack := []span{{low: low, high: high}}
	for len(stack) > 0 {
		n := len(stack) - 1
		low := stack[n].low
		high := stack[n].high
		stack = stack[:n]

		for {
			if high-low <= 16 {
				insertionSortQueueDOS(queue, low, high, t)
				break
			}

			i := low
			j := high
			pivot := t.nodes[queue[(low+high)/2]/4].weight
			for {
				for t.nodes[queue[i]/4].weight < pivot {
					i++
				}
				for t.nodes[queue[j]/4].weight > pivot {
					j--
				}
				if j >= i {
					queue[i], queue[j] = queue[j], queue[i]
					i++
					j--
				}
				if j < i {
					break
				}
			}

			leftSize := j - low
			rightSize := high - i
			if rightSize > leftSize {
				if i < high {
					stack = append(stack, span{low: i, high: high})
				}
				high = j
			} else {
				if low < j {
					stack = append(stack, span{low: low, high: j})
				}
				low = i
			}
			if low >= high {
				break
			}
		}
	}
}

func insertionSortQueueDOS(queue []uint16, left, right int, t *huffTable) {
	for i := left + 1; i <= right; i++ {
		key := queue[i]
		keyWeight := t.nodes[key/4].weight
		insert := left
		for insert < i && t.nodes[queue[insert]/4].weight < keyWeight {
			insert++
		}
		copy(queue[insert+1:i+1], queue[insert:i])
		queue[insert] = key
	}
}

func (t *huffTable) decode(br *bitReader) (int, error) {
	buf, err := br.peek16()
	if err != nil {
		return 0, err
	}
	prefix := int(buf >> 7)
	nodeID := t.fastSym[prefix]
	nbits := int(t.fastNBits[prefix])
	if err := br.consume(nbits); err != nil {
		return 0, err
	}

	for nodeID >= internalNodeThreshold {
		bit, err := br.readBits(1)
		if err != nil {
			return 0, err
		}
		if bit != 0 {
			nodeID = t.nodes[nodeID/4].child0
		} else {
			nodeID = t.nodes[nodeID/4].child1
		}
	}

	return int(nodeID >> 2), nil
}
