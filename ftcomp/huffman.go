package ftcomp

import (
	"fmt"
	"sort"
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
		queue = append(queue, id)
	}
	if len(queue) == 0 {
		return nil, fmt.Errorf("%w: empty Huffman table", ErrInvalidData)
	}
	if len(queue) == 1 {
		t.nodes[lastZero/4].weight = 1
		queue = append(queue, lastZero)
	}

	sort.SliceStable(queue, func(i, j int) bool {
		return t.nodes[queue[i]/4].weight < t.nodes[queue[j]/4].weight
	})

	nextInternal := uint16(internalNodeThreshold)
	for len(queue) > 1 {
		left := queue[0]
		right := queue[1]
		queue = queue[2:]

		if int(nextInternal/4) >= len(t.nodes) {
			return nil, fmt.Errorf("%w: Huffman tree too large", ErrInvalidData)
		}
		parent := nextInternal
		nextInternal += 4

		p := &t.nodes[parent/4]
		p.weight = t.nodes[left/4].weight + t.nodes[right/4].weight
		p.parent = 0
		p.child0 = left
		p.child1 = right
		t.nodes[left/4].parent = parent
		t.nodes[right/4].parent = parent

		insert := len(queue)
		for i, id := range queue {
			if t.nodes[id/4].weight >= p.weight {
				insert = i
				break
			}
		}
		queue = append(queue, 0)
		copy(queue[insert+1:], queue[insert:])
		queue[insert] = parent
	}

	t.root = queue[0]
	for prefix := 0; prefix < fastSize; prefix++ {
		bits := uint16(prefix << 7)
		nbits := uint8(0)
		nodeID := t.root
		for nodeID >= internalNodeThreshold && nbits < fastBits {
			if bits&0x8000 != 0 {
				nodeID = t.nodes[nodeID/4].child1
			} else {
				nodeID = t.nodes[nodeID/4].child0
			}
			bits <<= 1
			nbits++
		}
		t.fastSym[prefix] = nodeID
		t.fastNBits[prefix] = nbits
	}

	return t, nil
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
			nodeID = t.nodes[nodeID/4].child1
		} else {
			nodeID = t.nodes[nodeID/4].child0
		}
	}

	return int(nodeID >> 2), nil
}
