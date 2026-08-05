package version

import "testing"

func TestSystemsPipSpecPinsReleaseModel(t *testing.T) {
	const want = "brevitas-systems==0.9.12"
	if got := SystemsPipSpec(); got != want {
		t.Fatalf("SystemsPipSpec() = %q, want %q", got, want)
	}
}

func TestAgentmapPipSpecPinsScanner(t *testing.T) {
	const want = "agentmap-scan==0.1.2"
	if got := AgentmapPipSpec(); got != want {
		t.Fatalf("AgentmapPipSpec() = %q, want %q", got, want)
	}
}
