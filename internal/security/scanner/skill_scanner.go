package scanner

import (
	"context"

	"github.com/julianshen/rubichan/internal/security"
)

// ScanFunc is a function type that performs a security scan and returns findings.
type ScanFunc func(ctx context.Context, target security.ScanTarget) ([]security.Finding, error)

// SkillScannerAdapter wraps a ScanFunc into a security.StaticScanner,
// allowing Security Rule Skills to be plugged into the scanner pipeline.
type SkillScannerAdapter struct {
	name   string
	scanFn ScanFunc
}

// NewSkillScannerAdapter creates a SkillScannerAdapter with the given name and function.
func NewSkillScannerAdapter(name string, fn ScanFunc) *SkillScannerAdapter {
	return &SkillScannerAdapter{
		name:   name,
		scanFn: fn,
	}
}

// Name returns the scanner name.
func (a *SkillScannerAdapter) Name() string {
	return a.name
}

// Scan delegates to the wrapped ScanFunc.
func (a *SkillScannerAdapter) Scan(ctx context.Context, target security.ScanTarget) ([]security.Finding, error) {
	return a.scanFn(ctx, target)
}
