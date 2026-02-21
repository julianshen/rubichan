package scanner

import (
	"context"
	"testing"

	"github.com/julianshen/rubichan/internal/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLicenseScannerName(t *testing.T) {
	s := NewLicenseScanner()
	assert.Equal(t, "license", s.Name())
}

func TestLicenseScannerInterface(t *testing.T) {
	var _ security.StaticScanner = NewLicenseScanner()
}

func TestLicenseScannerDetectsMIT(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "LICENSE", `MIT License

Copyright (c) 2026 Example Corp

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software.
`)

	s := NewLicenseScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)

	// MIT is permissive, so there should be no copyleft findings.
	for _, f := range findings {
		assert.NotContains(t, f.Title, "Copyleft",
			"MIT is permissive and should not trigger a copyleft finding")
	}
}

func TestLicenseScannerDetectsGPL(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "LICENSE", `GNU GENERAL PUBLIC LICENSE
Version 3, 29 June 2007

Copyright (C) 2007 Free Software Foundation, Inc.

Everyone is permitted to copy and distribute verbatim copies of this
license document, but changing it is not allowed.
`)

	s := NewLicenseScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	require.NotEmpty(t, findings)

	found := false
	for _, f := range findings {
		if f.Title == "Copyleft license detected: GPL" {
			found = true
			assert.Equal(t, security.SeverityMedium, f.Severity)
			assert.Equal(t, security.CategoryLicenseCompliance, f.Category)
			assert.Equal(t, "LICENSE", f.Location.File)
		}
	}
	assert.True(t, found, "expected a GPL copyleft finding")
}

func TestLicenseScannerMissingLicense(t *testing.T) {
	dir := t.TempDir()
	// No LICENSE file at all.

	s := NewLicenseScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	require.NotEmpty(t, findings)

	found := false
	for _, f := range findings {
		if f.Title == "Missing LICENSE file" {
			found = true
			assert.Equal(t, security.SeverityLow, f.Severity)
			assert.Equal(t, security.CategoryLicenseCompliance, f.Category)
		}
	}
	assert.True(t, found, "expected a missing license finding")
}

func TestLicenseScannerDetectsHeader(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "LICENSE", `MIT License

Copyright (c) 2026 Example Corp
`)
	writeFile(t, dir, "main.go", `// Copyright 2026 Example Corp. All rights reserved.
// Licensed under the MIT License.
package main

func main() {}
`)

	s := NewLicenseScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)

	found := false
	for _, f := range findings {
		if f.Title == "License header found in source file" {
			found = true
			assert.Equal(t, security.SeverityInfo, f.Severity)
			assert.Equal(t, "main.go", f.Location.File)
			assert.Equal(t, 1, f.Location.StartLine)
		}
	}
	assert.True(t, found, "expected a license header finding")
}

func TestLicenseScannerContextCancellation(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "LICENSE", "MIT License\n")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s := NewLicenseScanner()
	_, err := s.Scan(ctx, security.ScanTarget{RootDir: dir})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cancelled")
}

func TestLicenseScannerUnknownLicense(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "LICENSE", `This is a custom proprietary license.
All rights reserved.
No permission granted.
`)

	s := NewLicenseScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	// Unknown license should not trigger copyleft finding but shouldn't trigger
	// "missing license" either since the file exists.
	for _, f := range findings {
		assert.NotContains(t, f.Title, "Copyleft")
		assert.NotEqual(t, "Missing LICENSE file", f.Title)
	}
}

func TestLicenseScannerNoHeaderInFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "LICENSE", "MIT License\n")
	writeFile(t, dir, "main.go", `package main

import "fmt"

func main() {
	fmt.Println("hello")
}
`)

	s := NewLicenseScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	// No header finding expected since there's no copyright/license text.
	for _, f := range findings {
		assert.NotEqual(t, "License header found in source file", f.Title)
	}
}

func TestLicenseScannerHeaderContextCancellation(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "LICENSE", "MIT License\n")
	writeFile(t, dir, "main.go", `// Copyright 2026 Example Corp
package main
func main() {}
`)

	// Cancel context after license file check but source header scan may still cancel.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s := NewLicenseScanner()
	_, err := s.Scan(ctx, security.ScanTarget{RootDir: dir})
	assert.Error(t, err)
}

func TestLicenseScannerIdentifyLicenseNilForUnknown(t *testing.T) {
	lt := identifyLicense("Some custom text without any license keywords")
	assert.Nil(t, lt)
}

func TestLicenseScannerDetectsLGPL(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "LICENCE.txt", `GNU LESSER GENERAL PUBLIC LICENSE
Version 2.1, February 1999
`)

	s := NewLicenseScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)

	found := false
	for _, f := range findings {
		if f.Title == "Copyleft license detected: LGPL" {
			found = true
			assert.Equal(t, security.SeverityMedium, f.Severity)
		}
	}
	assert.True(t, found, "expected an LGPL copyleft finding")
}

func TestLicenseScannerDetectsAGPL(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "COPYING", `GNU AFFERO GENERAL PUBLIC LICENSE
Version 3, 19 November 2007
`)

	s := NewLicenseScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)

	found := false
	for _, f := range findings {
		if f.Title == "Copyleft license detected: AGPL" {
			found = true
			assert.Equal(t, security.SeverityMedium, f.Severity)
		}
	}
	assert.True(t, found, "expected an AGPL copyleft finding")
}
