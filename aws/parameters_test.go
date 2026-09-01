package aws

import "testing"

func TestParameterPathForcesTheHierarchicalForm(t *testing.T) {
	// Parameter Store rejects a name that holds a "/" without starting with one,
	// which is the shape every prefixed name has ("prod/" + "smtp/<tenant>").
	// Normalising all of them keeps one form, so a stored reference is
	// recognisable by its leading "/" whatever it names.
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"nested name gains a leading slash", "prod/smtp/tenant-1", "/prod/smtp/tenant-1"},
		{"already a path is left alone", "/prod/smtp/tenant-1", "/prod/smtp/tenant-1"},
		{"flat name becomes a path too", "smtp", "/smtp"},
		{"surrounding space is not part of the name", "  prod/smtp  ", "/prod/smtp"},
		{"empty stays empty, for Store to reject", "", ""},
		{"blank is empty, not a bare slash", "   ", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parameterPath(tt.in); got != tt.want {
				t.Errorf("parameterPath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestParameterStoreAppliesOptions(t *testing.T) {
	client := newTestAWS(Config{Region: "us-east-1"})
	if err := client.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	store := client.ParameterStore(
		WithParameterNamePrefix("  prod/  "),
		WithParameterKeyID("  alias/notification  "),
	)

	// Both options trim, so a prefix indented in a config file does not become
	// part of every parameter name.
	if store.namePrefix != "prod/" {
		t.Errorf("namePrefix = %q, want %q", store.namePrefix, "prod/")
	}
	if store.keyID != "alias/notification" {
		t.Errorf("keyID = %q, want %q", store.keyID, "alias/notification")
	}
}

func TestParameterStoreBuildsTheClientOnDemand(t *testing.T) {
	// SSM is not among the requested services, so the client does not exist
	// until ParameterStore asks for one — the same lazy build SecretStore gets,
	// so that wiring a store costs nothing until something stores a secret.
	client := newTestAWS(Config{Region: "us-east-1"}, S3)
	if err := client.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if client.SSMClient != nil {
		t.Fatal("SSMClient built by Init without the SSM service requested")
	}

	first := client.ParameterStore()
	if client.SSMClient == nil {
		t.Fatal("ParameterStore did not build the SSM client")
	}

	// Each call is a fresh wrapper over the same client, so options can differ
	// per use without opening a second connection.
	second := client.ParameterStore(WithParameterNamePrefix("staging/"))
	if first == second {
		t.Error("ParameterStore returned the same wrapper twice")
	}
	if first.client != second.client {
		t.Error("ParameterStore built a second SSM client")
	}
}

func TestInitBuildsTheSSMClientWhenRequested(t *testing.T) {
	client := newTestAWS(Config{Region: "us-east-1"}, SSM)
	if err := client.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if client.SSMClient == nil {
		t.Fatal("Init did not build the SSM client for the SSM service")
	}
}
