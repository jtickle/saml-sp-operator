package render

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
)

// Hash computes a deterministic sha256 digest over files, encoding each
// entry as a 4-byte big-endian length prefix of Name, the Name bytes, a
// 4-byte big-endian length prefix of Bytes, then the Bytes themselves
// (RESEARCH.md Pattern 3). Length-prefixing removes the naive-concatenation
// ambiguity where two different (filename, content) splits — e.g.
// ("ab","c") vs ("a","bc") — would otherwise collide.
//
// Hash does NOT sort files: the caller passes a fixed explicit order
// (shibboleth2.xml, nginx.conf, attribute-map.xml) so the hash input order
// is self-documenting at the call site rather than hidden inside this
// function. attribute-map.xml MUST be included by the caller — it is
// reloadChanges=false in shibboleth2.xml, so an attribute-only change would
// otherwise silently skip a required pod roll (D-08).
//
// This hash is a change-detection value for Phase 2's pod-template
// annotation gate, not an integrity/authenticity control — there is no
// adversary it needs to resist (RESEARCH.md Security Domain V6); do not add
// HMAC/keyed hashing here.
func Hash(files []ConfigFile) string {
	h := sha256.New()
	for _, f := range files {
		var lenBuf [4]byte
		binary.BigEndian.PutUint32(lenBuf[:], uint32(len(f.Name)))
		h.Write(lenBuf[:])
		h.Write([]byte(f.Name))
		binary.BigEndian.PutUint32(lenBuf[:], uint32(len(f.Bytes)))
		h.Write(lenBuf[:])
		h.Write(f.Bytes)
	}
	return hex.EncodeToString(h.Sum(nil))
}
