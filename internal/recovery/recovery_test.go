package recovery

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestWriteDigestValueFramesValues(t *testing.T) {
	digest := sha256.New()
	writeDigestValue(digest, "ab")
	writeDigestValue(digest, "c")
	got := hex.EncodeToString(digest.Sum(nil))

	different := sha256.New()
	writeDigestValue(different, "a")
	writeDigestValue(different, "bc")
	if got == hex.EncodeToString(different.Sum(nil)) {
		t.Fatal("length framing did not distinguish value boundaries")
	}
}
