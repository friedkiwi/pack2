package ftcomp

import (
	"encoding/binary"
	"fmt"
	"os"
	"sort"
	"testing"
)

func TestDebugEvaluateIntermediate(t *testing.T) {
	if os.Getenv("FTCOMP_DEBUG_EVALUATE") == "" {
		t.Skip("debug only")
	}
	data, err := os.ReadFile("../original/examples/EVALUATE.LI_")
	if err != nil {
		t.Fatal(err)
	}
	stream := data[58:]
	blockData := stream[4:]
	block := compressedBlock{
		intermediateTarget: int(binary.LittleEndian.Uint16(blockData[:2])),
		literalWeightA:     blockData[2],
		markerWeightA:      blockData[3],
		markerWeightB:      blockData[4],
		literalWeightB:     blockData[5],
		bitstream:          blockData[6:],
		version:            Version1,
	}
	br := newBitReader(block.bitstream)
	staticTable, err := buildHuffTable(staticWeights)
	if err != nil {
		t.Fatal(err)
	}
	model := make([]uint16, modelSymbolCount)
	for i := 0; i < modelSymbolCount; {
		sym, err := staticTable.decode(br)
		if err != nil {
			t.Fatal(err)
		}
		if sym == 0x100 {
			i += min(16, modelSymbolCount-i)
			continue
		}
		model[i] = uint16(byte(sym))
		i++
	}
	modelPos := br.pos
	modelConsumed := br.consumedBytes()
	writeDebugU16s(t, "/private/tmp/go_model_words.bin", model)
	tableA, err := buildAdaptiveTable(model, block.literalWeightA, block.markerWeightA)
	if err != nil {
		t.Fatal(err)
	}
	writeDebugU16s(t, "/private/tmp/go_weights_A.bin", debugAdaptiveWeights(model, block.literalWeightA, block.markerWeightA))
	writeDebugFastTable(t, "/private/tmp/go_primary_fast_A", tableA)
	tableB, err := buildAdaptiveTable(model, block.literalWeightB, block.markerWeightB)
	if err != nil {
		t.Fatal(err)
	}
	writeDebugU16s(t, "/private/tmp/go_weights_B.bin", debugAdaptiveWeights(model, block.literalWeightB, block.markerWeightB))
	writeDebugFastTable(t, "/private/tmp/go_primary_fast_B", tableB)
	suffixTable, err := buildHuffTable(taggedWeights)
	if err != nil {
		t.Fatal(err)
	}
	intermediate, err := decodeIntermediate(br, tableA, tableB, suffixTable, block)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("model bytes=%d consumed=%d total bytes=%d consumed=%d len=%d prefix=% x", modelPos, modelConsumed, br.pos, br.consumedBytes(), len(intermediate), intermediate[:min(len(intermediate), 96)])
	if err := os.WriteFile("/private/tmp/evaluate.intermediate", intermediate, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeDebugU16s(t *testing.T, path string, values []uint16) {
	t.Helper()
	buf := make([]byte, len(values)*2)
	for i, v := range values {
		binary.LittleEndian.PutUint16(buf[i*2:], v)
	}
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeDebugFastTable(t *testing.T, prefix string, table *huffTable) {
	t.Helper()
	sym := make([]byte, len(table.fastSym)*2)
	for i, v := range table.fastSym {
		binary.LittleEndian.PutUint16(sym[i*2:], v)
	}
	if err := os.WriteFile(prefix+"_sym.bin", sym, 0o644); err != nil {
		t.Fatal(err)
	}
	bits := make([]byte, len(table.fastNBits))
	copy(bits, table.fastNBits[:])
	if err := os.WriteFile(prefix+"_bits.bin", bits, 0o644); err != nil {
		t.Fatal(err)
	}
}

func debugAdaptiveWeights(model []uint16, literalWeight, markerWeight byte) []uint16 {
	weights := make([]uint16, modelSymbolCount)
	maxWeight := 0
	for sym, m := range model {
		if m == 0 {
			continue
		}
		scale := literalWeight
		if symbolClass[sym] != 0 {
			scale = markerWeight
		}
		w := int(m) * int(scale)
		weights[sym] = uint16(w)
		if w > maxWeight {
			maxWeight = w
		}
	}
	if maxWeight > 0xff {
		scale := 0xffff / maxWeight
		for i, w := range weights {
			if w == 0 {
				continue
			}
			scaled := int(uint16(int(w)*scale) >> 8)
			if scaled == 0 {
				scaled = 1
			}
			weights[i] = uint16(scaled)
		}
	}
	return weights
}

func TestDebugEvaluateWeightOrders(t *testing.T) {
	if os.Getenv("FTCOMP_DEBUG_EVALUATE") == "" {
		t.Skip("debug only")
	}
	data, err := os.ReadFile("../original/examples/EVALUATE.LI_")
	if err != nil {
		t.Fatal(err)
	}
	stream := data[58:]
	blockData := stream[4:]
	block := compressedBlock{
		intermediateTarget: int(binary.LittleEndian.Uint16(blockData[:2])),
		literalWeightA:     blockData[2],
		markerWeightA:      blockData[3],
		markerWeightB:      blockData[4],
		literalWeightB:     blockData[5],
		bitstream:          blockData[6:],
		version:            Version1,
	}
	staticTable, err := buildHuffTable(staticWeights)
	if err != nil {
		t.Fatal(err)
	}
	suffixTable, err := buildHuffTable(taggedWeights)
	if err != nil {
		t.Fatal(err)
	}
	for _, order := range []struct {
		name  string
		aLit  byte
		aMark byte
		bLit  byte
		bMark byte
	}{
		{"current", blockData[2], blockData[3], blockData[5], blockData[4]},
		{"all-file-order", blockData[2], blockData[3], blockData[4], blockData[5]},
		{"swap-a", blockData[3], blockData[2], blockData[5], blockData[4]},
		{"swap-b", blockData[2], blockData[3], blockData[4], blockData[5]},
		{"both-swapped", blockData[3], blockData[2], blockData[4], blockData[5]},
	} {
		br := newBitReader(block.bitstream)
		model := make([]uint16, modelSymbolCount)
		for i := 0; i < modelSymbolCount; {
			sym, err := staticTable.decode(br)
			if err != nil {
				t.Fatal(err)
			}
			if sym == 0x100 {
				i += min(16, modelSymbolCount-i)
				continue
			}
			model[i] = uint16(byte(sym))
			i++
		}
		tableA, err := buildAdaptiveTable(model, order.aLit, order.aMark)
		if err != nil {
			t.Fatal(err)
		}
		tableB, err := buildAdaptiveTable(model, order.bLit, order.bMark)
		if err != nil {
			t.Fatal(err)
		}
		intermediate, err := decodeIntermediate(br, tableA, tableB, suffixTable, block)
		if err != nil {
			t.Logf("%s: err=%v", order.name, err)
		} else {
			logDebugIntermediate(t, order.name, br, intermediate, block.version)
		}

		br = newBitReader(block.bitstream)
		for i := 0; i < modelSymbolCount; {
			sym, err := staticTable.decode(br)
			if err != nil {
				t.Fatal(err)
			}
			if sym == 0x100 {
				i += min(16, modelSymbolCount-i)
				continue
			}
			i++
		}
		intermediate, err = decodeIntermediate(br, tableB, tableA, suffixTable, block)
		if err != nil {
			t.Logf("%s/select-swapped: err=%v", order.name, err)
		} else {
			logDebugIntermediate(t, order.name+"/select-swapped", br, intermediate, block.version)
		}
	}
}

func logDebugIntermediate(t *testing.T, name string, br *bitReader, intermediate []byte, version int) {
	segmentLen := -1
	if len(intermediate) >= 2 {
		segmentLen = int(binary.LittleEndian.Uint16(intermediate[:2]))
	}
	t.Logf("%s: len=%d seg=%d consumed=%d prefix=% x", name, len(intermediate), segmentLen, br.consumedBytes(), intermediate[:min(len(intermediate), 32)])
	if segmentLen > 0 && segmentLen <= len(intermediate)-2 {
		expanded, err := expandFramedIntermediate(intermediate, version)
		t.Logf("%s: expand len=%d err=%v text=%q", name, len(expanded), err, fmt.Sprintf("% .16x", expanded[:min(len(expanded), 16)]))
	}
}

func TestDebugEvaluateHuffVariants(t *testing.T) {
	if os.Getenv("FTCOMP_DEBUG_EVALUATE") == "" {
		t.Skip("debug only")
	}
	data, err := os.ReadFile("../original/examples/EVALUATE.LI_")
	if err != nil {
		t.Fatal(err)
	}
	blockData := data[58+4:]
	block := compressedBlock{
		intermediateTarget: int(binary.LittleEndian.Uint16(blockData[:2])),
		literalWeightA:     blockData[2],
		markerWeightA:      blockData[3],
		markerWeightB:      blockData[4],
		literalWeightB:     blockData[5],
		bitstream:          blockData[6:],
		version:            Version1,
	}
	for sortMode := 0; sortMode < 4; sortMode++ {
		for insertAfterEqual := 0; insertAfterEqual < 2; insertAfterEqual++ {
			for childSwap := 0; childSwap < 2; childSwap++ {
				name := fmt.Sprintf("sort=%d afterEq=%d childSwap=%d", sortMode, insertAfterEqual, childSwap)
				staticTable, err := buildHuffTableVariant(staticWeights, sortMode, insertAfterEqual != 0, childSwap != 0)
				if err != nil {
					t.Logf("%s static err=%v", name, err)
					continue
				}
				br := newBitReader(block.bitstream)
				model := make([]uint16, modelSymbolCount)
				modelOK := true
				for i := 0; i < modelSymbolCount; {
					sym, err := staticTable.decode(br)
					if err != nil {
						t.Logf("%s model err=%v", name, err)
						modelOK = false
						break
					}
					if sym == 0x100 {
						i += min(16, modelSymbolCount-i)
						continue
					}
					if sym < 0 || sym > 0xff {
						t.Logf("%s model bad sym=%x i=%d", name, sym, i)
						modelOK = false
						break
					}
					model[i] = uint16(byte(sym))
					i++
				}
				if !modelOK {
					continue
				}
				tableA, err := buildAdaptiveTableVariant(model, block.literalWeightA, block.markerWeightA, sortMode, insertAfterEqual != 0, childSwap != 0)
				if err != nil {
					t.Logf("%s tableA err=%v", name, err)
					continue
				}
				tableB, err := buildAdaptiveTableVariant(model, block.literalWeightB, block.markerWeightB, sortMode, insertAfterEqual != 0, childSwap != 0)
				if err != nil {
					t.Logf("%s tableB err=%v", name, err)
					continue
				}
				suffixTable, err := buildHuffTableVariant(taggedWeights, sortMode, insertAfterEqual != 0, childSwap != 0)
				if err != nil {
					t.Logf("%s suffix err=%v", name, err)
					continue
				}
				intermediate, err := decodeIntermediate(br, tableA, tableB, suffixTable, block)
				if err != nil {
					t.Logf("%s decode err=%v", name, err)
					continue
				}
				if len(intermediate) >= 2 {
					seg := int(binary.LittleEndian.Uint16(intermediate[:2]))
					if seg <= len(intermediate)-2 {
						logDebugIntermediate(t, name, br, intermediate, block.version)
					} else {
						t.Logf("%s seg=%d prefix=% x", name, seg, intermediate[:min(len(intermediate), 8)])
					}
				}
			}
		}
	}
}

func buildAdaptiveTableVariant(model []uint16, literalWeight, markerWeight byte, sortMode int, insertAfterEqual bool, childSwap bool) (*huffTable, error) {
	weights := make([]uint16, modelSymbolCount)
	maxWeight := 0
	for sym, m := range model {
		if m == 0 {
			continue
		}
		scale := literalWeight
		if symbolClass[sym] != 0 {
			scale = markerWeight
		}
		w := int(m) * int(scale)
		weights[sym] = uint16(w)
		if w > maxWeight {
			maxWeight = w
		}
	}
	if maxWeight > 0xff {
		scale := 0xffff / maxWeight
		for i, w := range weights {
			if w == 0 {
				continue
			}
			scaled := int(uint16(int(w)*scale) >> 8)
			if scaled == 0 {
				scaled = 1
			}
			weights[i] = uint16(scaled)
		}
	}
	return buildHuffTableVariant(weights, sortMode, insertAfterEqual, childSwap)
}

func buildHuffTableVariant(weights []uint16, sortMode int, insertAfterEqual bool, childSwap bool) (*huffTable, error) {
	t := &huffTable{}
	queue := make([]uint16, 0, modelSymbolCount)
	var lastZero uint16
	oneCount := 0
	for sym, weight := range weights {
		id := uint16(sym * 4)
		t.nodes[sym].weight = weight
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
		return nil, fmt.Errorf("empty")
	}
	if len(queue) == 1 {
		t.nodes[lastZero/4].weight = 1
		queue = append(queue, lastZero)
	}
	switch sortMode {
	case 0:
		sortQueueDOS(queue, oneCount, len(queue)-1, t)
	case 1:
		sort.SliceStable(queue[oneCount:], func(i, j int) bool {
			return t.nodes[queue[oneCount+i]/4].weight < t.nodes[queue[oneCount+j]/4].weight
		})
	case 2:
		sort.Slice(queue[oneCount:], func(i, j int) bool {
			return t.nodes[queue[oneCount+i]/4].weight < t.nodes[queue[oneCount+j]/4].weight
		})
	case 3:
		sort.Slice(queue[oneCount:], func(i, j int) bool {
			return t.nodes[queue[oneCount+i]/4].weight > t.nodes[queue[oneCount+j]/4].weight
		})
	}

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
				w := t.nodes[queue[i]/4].weight
				if (!insertAfterEqual && w >= parentWeight) || (insertAfterEqual && w > parentWeight) {
					insert = i
					break
				}
			}
			parent := nextInternal
			copy(queue[queueRead:insert-1], queue[queueRead+1:insert])
			queue[insert-1] = parent
			nextInternal += 4
			p := &t.nodes[parent/4]
			p.weight = parentWeight
			if childSwap {
				p.child0 = right
				p.child1 = left
			} else {
				p.child0 = left
				p.child1 = right
			}
			t.nodes[left/4].parent = parent
			t.nodes[right/4].parent = parent
			if remaining == 2 {
				break
			}
		}
	}
	left := queue[queueRead]
	right := queue[queueRead+1]
	t.root = nextInternal
	p := &t.nodes[t.root/4]
	p.weight = t.nodes[left/4].weight + t.nodes[right/4].weight
	if childSwap {
		p.child0 = right
		p.child1 = left
	} else {
		p.child0 = left
		p.child1 = right
	}
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
