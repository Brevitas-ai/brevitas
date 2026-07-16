package optimizer

import (
	"bytes"
	"testing"
)

func TestEmbeddedAdapterDoesNotMarkRetrievalLossless(t *testing.T) {
	if !bytes.Contains(AdapterScript, []byte(`"lossy": strategy == "retrieve"`)) {
		t.Fatal("embedded optimizer must classify context retrieval as quality-affecting")
	}
}
