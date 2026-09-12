package main

import (
	"encoding/binary"
	"encoding/hex"
	"math/bits"
)

// SHA3-256, because that is the digest the CMake File API names its object
// files by. The standard library of the Go release this repository builds
// with has no SHA-3 and the vendored module set has none either, and pulling
// in a cryptography module so that a fixture tool can reproduce a file name
// would be a poor trade. The implementation below is the reference Keccak
// permutation; replynorm verifies it against the corpus on every run, by
// recomputing the name of a reply file it did not touch.

var keccakRoundConstants = [24]uint64{
	0x0000000000000001, 0x0000000000008082, 0x800000000000808a, 0x8000000080008000,
	0x000000000000808b, 0x0000000080000001, 0x8000000080008081, 0x8000000000008009,
	0x000000000000008a, 0x0000000000000088, 0x0000000080008009, 0x000000008000000a,
	0x000000008000808b, 0x800000000000008b, 0x8000000000008089, 0x8000000000008003,
	0x8000000000008002, 0x8000000000000080, 0x000000000000800a, 0x800000008000000a,
	0x8000000080008081, 0x8000000000008080, 0x0000000080000001, 0x8000000080008008,
}

// The lane visited by each step of rho-pi, and the rotation applied to it.
var (
	keccakPiLane    = [24]int{10, 7, 11, 17, 18, 3, 5, 16, 8, 21, 24, 4, 15, 23, 19, 13, 12, 2, 20, 14, 22, 9, 6, 1}
	keccakRhoOffset = [24]int{1, 3, 6, 10, 15, 21, 28, 36, 45, 55, 2, 14, 27, 41, 56, 8, 25, 43, 62, 18, 39, 61, 20, 44}
)

func keccakF1600(state *[25]uint64) {
	var lanes [5]uint64
	for round := 0; round < 24; round++ {
		// Theta.
		for x := 0; x < 5; x++ {
			lanes[x] = state[x] ^ state[x+5] ^ state[x+10] ^ state[x+15] ^ state[x+20]
		}
		for x := 0; x < 5; x++ {
			column := lanes[(x+4)%5] ^ bits.RotateLeft64(lanes[(x+1)%5], 1)
			for y := 0; y < 25; y += 5 {
				state[x+y] ^= column
			}
		}
		// Rho and pi.
		carried := state[1]
		for step := 0; step < 24; step++ {
			lane := keccakPiLane[step]
			next := state[lane]
			state[lane] = bits.RotateLeft64(carried, keccakRhoOffset[step])
			carried = next
		}
		// Chi.
		for y := 0; y < 25; y += 5 {
			copy(lanes[:], state[y:y+5])
			for x := 0; x < 5; x++ {
				state[y+x] = lanes[x] ^ (^lanes[(x+1)%5] & lanes[(x+2)%5])
			}
		}
		// Iota.
		state[0] ^= keccakRoundConstants[round]
	}
}

// sha3Rate is the sponge rate of SHA3-256 in bytes: 200 minus twice the
// 32-byte digest.
const sha3Rate = 136

func sha3Sum256(message []byte) []byte {
	var state [25]uint64
	for len(message) >= sha3Rate {
		absorbBlock(&state, message[:sha3Rate])
		keccakF1600(&state)
		message = message[sha3Rate:]
	}
	var final [sha3Rate]byte
	copy(final[:], message)
	final[len(message)] = 0x06 // the SHA-3 domain separator, then the pad
	final[sha3Rate-1] |= 0x80
	absorbBlock(&state, final[:])
	keccakF1600(&state)

	digest := make([]byte, 32)
	for lane := 0; lane < 4; lane++ {
		binary.LittleEndian.PutUint64(digest[lane*8:], state[lane])
	}
	return digest
}

func absorbBlock(state *[25]uint64, block []byte) {
	for lane := 0; lane < len(block)/8; lane++ {
		state[lane] ^= binary.LittleEndian.Uint64(block[lane*8:])
	}
}

// replyHash is the name suffix the File API gives a document: the first ten
// bytes of its SHA3-256, in hex.
func replyHash(content []byte) string {
	return hex.EncodeToString(sha3Sum256(content)[:10])
}
