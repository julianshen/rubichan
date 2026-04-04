package verification

import "testing"

func TestDependencyResolutionCommandRecognizesSharedPatterns(t *testing.T) {
	tests := []string{
		"npm ci",
		"pnpm install",
		"yarn install --frozen-lockfile",
		"go mod tidy",
		"gradle build",
		"gradlew build",
		"./gradlew build",
		"python -m pip install -r requirements.txt",
		"python3 -m pip install fastapi",
		"uv sync",
		"poetry install --no-interaction",
		"mvn -q package",
	}
	for _, command := range tests {
		if got := DependencyResolutionCommand(command); got == "" {
			t.Fatalf("expected %q to be recognized", command)
		}
	}
}

func TestLooksLikeDependencyCommandIntentRecognizesGradleIntent(t *testing.T) {
	if !LooksLikeDependencyCommandIntent("./gradlew dependencies") {
		t.Fatalf("expected gradle intent to be recognized")
	}
}
