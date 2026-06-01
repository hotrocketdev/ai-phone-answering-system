package audio

import (
	"encoding/binary"
	"fmt"

	g722codec "github.com/gotranspile/g722"
)

const (
	Samples16k         = 320 // 16000 * 0.020
	FrameSizePCM16_16k = 640 // 16000 Hz * 0.020s * 2 bytes
	FrameSizeG722_16k  = 160 // 64 kbps G.722, 20 ms
)

type G722Encoder struct {
	enc *g722codec.Encoder
	buf []byte
}

func NewG722Encoder() *G722Encoder {
	return &G722Encoder{enc: g722codec.NewEncoder(g722codec.RateDefault, 0)}
}

func (e *G722Encoder) EncodePCM16Frame(pcm16 []byte) ([]byte, error) {
	if len(pcm16) != FrameSizePCM16_16k {
		return nil, fmt.Errorf("g722: expected %d PCM16 bytes, got %d", FrameSizePCM16_16k, len(pcm16))
	}
	samples := make([]int16, Samples16k)
	for i := 0; i < Samples16k; i++ {
		samples[i] = int16(binary.LittleEndian.Uint16(pcm16[i*2:]))
	}
	out := make([]byte, FrameSizeG722_16k)
	n := e.enc.Encode(out, samples)
	return out[:n], nil
}

func (e *G722Encoder) ProcessPCM16Bytes(pcm16 []byte) ([][]byte, error) {
	e.buf = append(e.buf, pcm16...)
	var frames [][]byte
	for len(e.buf) >= FrameSizePCM16_16k {
		frame := e.buf[:FrameSizePCM16_16k]
		encoded, err := e.EncodePCM16Frame(frame)
		if err != nil {
			return nil, err
		}
		frames = append(frames, encoded)
		e.buf = e.buf[FrameSizePCM16_16k:]
	}
	return frames, nil
}

func (e *G722Encoder) Flush() ([][]byte, error) {
	if len(e.buf) == 0 {
		return nil, nil
	}
	for len(e.buf) < FrameSizePCM16_16k {
		e.buf = append(e.buf, 0)
	}
	return e.ProcessPCM16Bytes(nil)
}
