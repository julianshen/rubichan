package verification

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDependencyResolutionCommand_EmptyAndWhitespace(t *testing.T) {
	assert.Equal(t, "", DependencyResolutionCommand(""))
	assert.Equal(t, "", DependencyResolutionCommand("   "))
	assert.Equal(t, "", DependencyResolutionCommand("\t\n"))
}

func TestDependencyResolutionCommand_Mvn(t *testing.T) {
	// Matches: mvn followed by a recognized goal
	assert.Equal(t, "mvn dependency:resolve", DependencyResolutionCommand("mvn dependency:resolve"))
	assert.Equal(t, "mvn compile", DependencyResolutionCommand("mvn compile"))
	assert.Equal(t, "mvn package", DependencyResolutionCommand("mvn package"))
	assert.Equal(t, "mvn test", DependencyResolutionCommand("mvn test"))

	// mvn without recognized goal returns ""
	assert.Equal(t, "", DependencyResolutionCommand("mvn clean"))
	assert.Equal(t, "", DependencyResolutionCommand("mvn deploy"))
}

func TestDependencyResolutionCommand_NpmPnpmYarn(t *testing.T) {
	assert.Equal(t, "npm ci", DependencyResolutionCommand("npm ci"))
	assert.Equal(t, "npm install", DependencyResolutionCommand("npm install"))
	assert.Equal(t, "pnpm install", DependencyResolutionCommand("pnpm install"))
	assert.Equal(t, "yarn install", DependencyResolutionCommand("yarn install"))
}

func TestDependencyResolutionCommand_Go(t *testing.T) {
	assert.Equal(t, "go get ./...", DependencyResolutionCommand("go get ./..."))
	assert.Equal(t, "go mod tidy", DependencyResolutionCommand("go mod tidy"))
}

func TestDependencyResolutionCommand_GradlePython(t *testing.T) {
	assert.Equal(t, "gradle build", DependencyResolutionCommand("gradle build"))
	assert.Equal(t, "gradlew build", DependencyResolutionCommand("gradlew build"))
	assert.Equal(t, "./gradlew build", DependencyResolutionCommand("./gradlew build"))
	assert.Equal(t, "pip install -r requirements.txt", DependencyResolutionCommand("pip install -r requirements.txt"))
	assert.Equal(t, "uv sync", DependencyResolutionCommand("uv sync"))
	assert.Equal(t, "poetry install", DependencyResolutionCommand("poetry install"))
}

func TestDependencyResolutionCommand_NormalizesCase(t *testing.T) {
	assert.Equal(t, "npm install", DependencyResolutionCommand("NPM INSTALL"))
	assert.Equal(t, "go mod tidy", DependencyResolutionCommand("  Go Mod Tidy  "))
}

func TestDependencyResolutionCommand_UnrelatedCommand(t *testing.T) {
	assert.Equal(t, "", DependencyResolutionCommand("ls -la"))
	assert.Equal(t, "", DependencyResolutionCommand("echo hello"))
	assert.Equal(t, "", DependencyResolutionCommand("rm -rf /"))
}

func TestLooksLikeDependencyCommandIntent_Positive(t *testing.T) {
	cases := []string{
		"brew install foo",
		"npm i react",
		"pnpm i",
		"yarn add lodash",
		"pip freeze",
		"go mod why",
		"go get ./...",
		"mvn verify",
		"gradle assemble",
		"./gradlew check",
	}
	for _, c := range cases {
		assert.Truef(t, LooksLikeDependencyCommandIntent(c), "expected %q to look like dep intent", c)
	}
}

func TestLooksLikeDependencyCommandIntent_Negative(t *testing.T) {
	cases := []string{
		"ls",
		"echo hello",
		"git status",
		"cat README.md",
	}
	for _, c := range cases {
		assert.Falsef(t, LooksLikeDependencyCommandIntent(c), "did not expect %q to look like dep intent", c)
	}
}
