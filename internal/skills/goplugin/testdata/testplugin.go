// Package main provides a sample Go plugin for testing the Go plugin backend.
// This file is the source for a test .so plugin. It is NOT compiled during
// normal test runs -- it exists as reference material and can be built with:
//
//	go build -buildmode=plugin -o testplugin.so testdata/testplugin.go
//
// For unit tests, we use a mock-based PluginLoader instead.
package main

import "github.com/julianshen/rubichan/pkg/skillsdk"

// testPlugin is a minimal SkillPlugin implementation for testing.
type testPlugin struct{}

func (p *testPlugin) Manifest() skillsdk.Manifest {
	return skillsdk.Manifest{
		Name:        "test-plugin",
		Version:     "1.0.0",
		Description: "A test plugin",
		Author:      "test",
		License:     "MIT",
	}
}

func (p *testPlugin) Activate(ctx skillsdk.Context) error {
	return nil
}

func (p *testPlugin) Deactivate(ctx skillsdk.Context) error {
	return nil
}

// NewSkill is the exported symbol that the Go plugin backend looks up.
// It must return a skillsdk.SkillPlugin.
func NewSkill() skillsdk.SkillPlugin {
	return &testPlugin{}
}
