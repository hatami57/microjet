package core

import "testing"

func TestNewViperDoesNotErrorWithNoFile(t *testing.T) {
	if _, err := newViper(""); err != nil {
		t.Fatalf("NewViper returned error with no config file: %v", err)
	}
}

