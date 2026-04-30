package uiauto

import "testing"

func TestWSLTierMapping(t *testing.T) {
	tests := []struct {
		input ModelTier
		want  ModelTier
	}{
		{TierLight, ModelTierWSLFast},
		{TierSmart, ModelTierWSLSmart},
		{TierVLM, ModelTierWSLPowerful},
		{ModelTierWSLFast, ModelTierWSLFast},
	}

	for _, tc := range tests {
		got := ToWSLTier(tc.input)
		if got != tc.want {
			t.Errorf("ToWSLTier(%s) = %s, want %s", tc.input, got, tc.want)
		}
	}
}

func TestIsWSLTier(t *testing.T) {
	if TierLight.IsWSLTier() {
		t.Error("TierLight should not be WSL")
	}
	if !ModelTierWSLFast.IsWSLTier() {
		t.Error("ModelTierWSLFast should be WSL")
	}
	if !ModelTierWSLSmart.IsWSLTier() {
		t.Error("ModelTierWSLSmart should be WSL")
	}
	if !ModelTierWSLPowerful.IsWSLTier() {
		t.Error("ModelTierWSLPowerful should be WSL")
	}
}

func TestWSLTierString(t *testing.T) {
	if ModelTierWSLFast.String() != "wsl-fast" {
		t.Errorf("got %s, want wsl-fast", ModelTierWSLFast.String())
	}
	if ModelTierWSLSmart.String() != "wsl-smart" {
		t.Errorf("got %s, want wsl-smart", ModelTierWSLSmart.String())
	}
	if ModelTierWSLPowerful.String() != "wsl-powerful" {
		t.Errorf("got %s, want wsl-powerful", ModelTierWSLPowerful.String())
	}
}
