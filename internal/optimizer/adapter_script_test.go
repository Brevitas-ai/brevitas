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

func TestEmbeddedAdapterFeedsProviderReceiptsBackToRouter(t *testing.T) {
	if !bytes.Contains(AdapterScript, []byte(`_record_engine_usage(`)) {
		t.Fatal("embedded optimizer must feed real cache reads/writes back to the ROI guard")
	}
}
