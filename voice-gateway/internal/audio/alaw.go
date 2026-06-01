package audio

import "encoding/binary"

// AlawToPCM16 converts a G.711 A-law byte slice to PCM16 little-endian samples.
// pcm16 must be at least len(alaw)*2 bytes.
func AlawToPCM16(alaw []byte, pcm16 []byte) {
	for i := 0; i < len(alaw); i++ {
		sample := decodeAlaw(alaw[i])
		binary.LittleEndian.PutUint16(pcm16[i*2:], uint16(sample))
	}
}

func decodeAlaw(alaw byte) int16 {
	alaw ^= 0x55

	t := int16(alaw&0x0f) << 4
	segment := (alaw & 0x70) >> 4
	switch segment {
	case 0:
		t += 8
	case 1:
		t += 0x108
	default:
		t += 0x108
		t <<= segment - 1
	}

	if alaw&0x80 != 0 {
		return t
	}
	return -t
}
