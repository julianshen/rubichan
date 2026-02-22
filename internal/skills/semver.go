package skills

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// IsSemVerRange returns true if the version string is a SemVer range
// constraint (e.g., "^1.0.0", "~1.2", ">=1.0.0, <2.0.0") rather than
// an exact version or "latest".
func IsSemVerRange(version string) bool {
	if version == "" || version == "latest" {
		return false
	}
	// Range operators that indicate a constraint rather than exact version.
	return strings.ContainsAny(version, "^~><!=, ")
}

// ResolveVersion selects the highest version from available that satisfies
// the given constraint. The constraint can be:
//   - "latest": returns the highest available version
//   - An exact version like "1.2.3": returns it if available
//   - A SemVer range like "^1.0.0", "~1.2", ">=1.0.0, <2.0.0"
func ResolveVersion(constraint string, available []string) (string, error) {
	if len(available) == 0 {
		return "", fmt.Errorf("no versions available")
	}

	// Parse all available versions, tracking unparseable ones.
	var versions []*semver.Version
	var skipped []string
	for _, v := range available {
		sv, err := semver.NewVersion(v)
		if err != nil {
			skipped = append(skipped, v)
			continue
		}
		versions = append(versions, sv)
	}

	if len(versions) == 0 {
		if len(skipped) > 0 {
			return "", fmt.Errorf("no valid semver versions in available list (skipped unparseable: %s)", strings.Join(skipped, ", "))
		}
		return "", fmt.Errorf("no valid semver versions in available list")
	}

	// Sort descending so highest version is first.
	sort.Sort(sort.Reverse(semver.Collection(versions)))

	// "latest" returns the highest stable version, falling back to
	// the highest pre-release if no stable versions exist.
	if constraint == "latest" {
		for _, v := range versions {
			if v.Prerelease() == "" {
				return v.Original(), nil
			}
		}
		return versions[0].Original(), nil
	}

	// Try parsing as exact version first.
	if !IsSemVerRange(constraint) {
		exact, err := semver.NewVersion(constraint)
		if err != nil {
			return "", fmt.Errorf("invalid version %q: %w", constraint, err)
		}
		for _, v := range versions {
			if v.Equal(exact) {
				return v.Original(), nil
			}
		}
		return "", fmt.Errorf("version %q not found in available versions", constraint)
	}

	// Parse as a constraint range.
	c, err := semver.NewConstraint(constraint)
	if err != nil {
		return "", fmt.Errorf("invalid version constraint %q: %w", constraint, err)
	}

	// Find the highest matching version (already sorted descending).
	for _, v := range versions {
		if c.Check(v) {
			return v.Original(), nil
		}
	}

	msg := fmt.Sprintf("no version matching %q (available: %s)", constraint, strings.Join(available, ", "))
	if len(skipped) > 0 {
		msg += fmt.Sprintf("; skipped %d unparseable: %s", len(skipped), strings.Join(skipped, ", "))
	}
	return "", errors.New(msg)
}
