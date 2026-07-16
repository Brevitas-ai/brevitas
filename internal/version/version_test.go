package version

import "testing"

func TestSystemsPipSpecPinsReleaseModel(t *testing.T) {
	const want = "brevitas-systems==0.9.11"
	if got := SystemsPipSpec(); got != want {
		t.Fatalf("SystemsPipSpec() = %q, want %q", got, want)
	}
}
