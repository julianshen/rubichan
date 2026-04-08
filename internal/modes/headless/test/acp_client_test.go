package headless_test

import (
	"testing"

	"github.com/julianshen/rubichan/internal/modes/headless"
)

func TestHeadlessClientRunCodeReview(t *testing.T) {
	client := headless.NewACPClient()

	resp, err := client.RunCodeReview("./src", 5)
	if err != nil {
		t.Fatalf("code review failed: %v", err)
	}

	_ = resp
}

func TestHeadlessClientRunSecurityScan(t *testing.T) {
	client := headless.NewACPClient()

	resp, err := client.RunSecurityScan("./", false)
	if err != nil {
		t.Fatalf("security scan failed: %v", err)
	}

	_ = resp
}

func TestHeadlessClientTimeout(t *testing.T) {
	client := headless.NewACPClient()
	client.SetTimeout(30)

	if client.Timeout() != 30 {
		t.Errorf("got timeout %d, want 30", client.Timeout())
	}
}

func TestHeadlessClientDefaultTimeout(t *testing.T) {
	client := headless.NewACPClient()

	if client.Timeout() != 60 {
		t.Errorf("got default timeout %d, want 60", client.Timeout())
	}
}
