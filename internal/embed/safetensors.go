package embed

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"sync"
)

// tensor is one entry of a safetensors file, converted to float32.
type tensor struct {
	shape []int
	data  []float32
}

// readSafetensors parses a safetensors file (8-byte little-endian header
// length, JSON header, raw little-endian data) and returns its F16 and F32
// tensors as float32. Tensors of other dtypes are skipped.
func readSafetensors(b []byte) (map[string]tensor, error) {
	if len(b) < 8 {
		return nil, fmt.Errorf("safetensors: file too short")
	}
	n := binary.LittleEndian.Uint64(b)
	if n > uint64(len(b)-8) {
		return nil, fmt.Errorf("safetensors: header length %d exceeds file size", n)
	}
	var header map[string]json.RawMessage
	if err := json.Unmarshal(b[8:8+n], &header); err != nil {
		return nil, fmt.Errorf("safetensors: header: %w", err)
	}
	data := b[8+n:]

	tensors := make(map[string]tensor, len(header))
	for name, raw := range header {
		if name == "__metadata__" {
			continue
		}
		var info struct {
			DType       string   `json:"dtype"`
			Shape       []int    `json:"shape"`
			DataOffsets [2]int64 `json:"data_offsets"`
		}
		if err := json.Unmarshal(raw, &info); err != nil {
			return nil, fmt.Errorf("safetensors: %s: %w", name, err)
		}
		begin, end := info.DataOffsets[0], info.DataOffsets[1]
		if begin < 0 || end < begin || end > int64(len(data)) {
			return nil, fmt.Errorf("safetensors: %s: bad data offsets %v", name, info.DataOffsets)
		}
		count := 1
		for _, d := range info.Shape {
			count *= d
		}
		raw := data[begin:end]
		t := tensor{shape: info.Shape, data: make([]float32, count)}
		switch info.DType {
		case "F16":
			if len(raw) != 2*count {
				return nil, fmt.Errorf("safetensors: %s: %d bytes for %d f16 values", name, len(raw), count)
			}
			table := f16Table()
			for i := range t.data {
				t.data[i] = table[binary.LittleEndian.Uint16(raw[2*i:])]
			}
		case "F32":
			if len(raw) != 4*count {
				return nil, fmt.Errorf("safetensors: %s: %d bytes for %d f32 values", name, len(raw), count)
			}
			for i := range t.data {
				t.data[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[4*i:]))
			}
		default:
			// Non-float tensors (e.g. the I64 position_ids buffer in the
			// original checkpoint) are not weights; a model that needs one
			// fails in Load with "missing tensor".
			continue
		}
		tensors[name] = t
	}
	return tensors, nil
}

// f16Table maps every half-precision bit pattern to its float32 value. Model
// loading converts ~23M weights; a table lookup was 1.4x faster than
// converting each one (same-run A/B, 2026-09-27), cutting load to ~70 ms.
var f16Table = sync.OnceValue(func() *[1 << 16]float32 {
	var t [1 << 16]float32
	for h := range t {
		t[h] = f16ToF32(uint16(h))
	}
	return &t
})

// f16ToF32 converts an IEEE 754 half-precision value to float32.
func f16ToF32(h uint16) float32 {
	sign := uint32(h>>15) << 31
	exp := uint32(h>>10) & 0x1f
	mant := uint32(h) & 0x3ff
	switch {
	case exp == 0 && mant == 0: // ±0
		return math.Float32frombits(sign)
	case exp == 0: // subnormal: normalize it
		e := uint32(127 - 15 + 1)
		for mant&0x400 == 0 {
			mant <<= 1
			e--
		}
		return math.Float32frombits(sign | e<<23 | (mant&0x3ff)<<13)
	case exp == 0x1f: // ±Inf, NaN
		return math.Float32frombits(sign | 0xff<<23 | mant<<13)
	default:
		return math.Float32frombits(sign | (exp+127-15)<<23 | mant<<13)
	}
}
