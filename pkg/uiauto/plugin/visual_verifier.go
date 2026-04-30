package plugin

import "context"

// VerificationResult summarises a visual-verifier outcome.
type VerificationResult struct {
	// Score is in [0,1]; 1.0 means the screenshot fully matches expectations.
	Score float64
	// Pass is the binary verdict; true when Score >= the verifier's threshold.
	Pass bool
	// Notes carries human-readable reasoning, useful for HTML reports.
	Notes string
}

// VisualVerifier scores a screenshot against an expected description or
// baseline. Implementations include OmniParser-only OCR matching, GPT-4V,
// and pixel-diff comparators.
type VisualVerifier interface {
	Verify(ctx context.Context, screenshot []byte, expectation string) (VerificationResult, error)
}

// NoopVisualVerifier returns Score=1.0, Pass=true regardless of input. Useful
// for public targets and unit tests where no visual verification is desired.
type NoopVisualVerifier struct{}

// NewNoopVisualVerifier returns the default no-op verifier.
func NewNoopVisualVerifier() *NoopVisualVerifier { return &NoopVisualVerifier{} }

// Verify returns a passing result with no notes.
func (NoopVisualVerifier) Verify(_ context.Context, _ []byte, _ string) (VerificationResult, error) {
	return VerificationResult{Score: 1.0, Pass: true}, nil
}
