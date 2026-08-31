package aws

import (
	"strings"
	"testing"
	"time"
)

// initedTestAWS returns a client that has been through Init, so DefaultConfig
// carries real credentials and a region.
func initedTestAWS(t *testing.T, config Config, services ...Service) *AWS {
	t.Helper()
	if config.AccessKey == "" {
		config.AccessKey, config.SecretKey = "AKIAEXAMPLE", "secret"
	}
	client := newTestAWS(config, services...)
	if err := client.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return client
}

// This is the test that earns the package its keep. Mutating DefaultConfig in
// place instead of copying it repoints DynamoDB and every other already-built
// client at the assumed account, and nothing else in the codebase would notice.
func TestAssumeRoleConfigDoesNotDisturbTheDefaultConfig(t *testing.T) {
	client := initedTestAWS(t, Config{Region: "eu-west-1"}, DynamoDB, SES)

	beforeRegion := client.DefaultConfig.Region
	beforeCreds := client.DefaultConfig.Credentials

	cfg, err := client.AssumeRoleConfig(AssumeRoleOptions{
		RoleARN: "arn:aws:iam::222222222222:role/Sender",
		Region:  "us-east-2",
	})
	if err != nil {
		t.Fatalf("AssumeRoleConfig: %v", err)
	}

	if client.DefaultConfig.Region != beforeRegion {
		t.Errorf("DefaultConfig.Region = %q, want it untouched at %q",
			client.DefaultConfig.Region, beforeRegion)
	}
	if client.DefaultConfig.Credentials != beforeCreds {
		t.Error("DefaultConfig.Credentials was replaced; every existing client now speaks for the assumed account")
	}
	if cfg.Region != "us-east-2" {
		t.Errorf("assumed config region = %q, want the role's own region", cfg.Region)
	}
	if cfg.Credentials == nil || cfg.Credentials == beforeCreds {
		t.Error("assumed config kept the application's own credentials")
	}
}

func TestAssumeRoleConfigKeepsOurRegionWhenNoneGiven(t *testing.T) {
	client := initedTestAWS(t, Config{Region: "eu-west-1"})

	cfg, err := client.AssumeRoleConfig(AssumeRoleOptions{
		RoleARN: "arn:aws:iam::222222222222:role/Sender",
	})
	if err != nil {
		t.Fatalf("AssumeRoleConfig: %v", err)
	}
	if cfg.Region != "eu-west-1" {
		t.Errorf("region = %q, want the configured %q", cfg.Region, "eu-west-1")
	}
}

func TestAssumeRoleConfigRequiresARoleARN(t *testing.T) {
	client := initedTestAWS(t, Config{Region: "eu-west-1"})

	if _, err := client.AssumeRoleConfig(AssumeRoleOptions{RoleARN: "  "}); err == nil {
		t.Fatal("expected an error when RoleARN is blank")
	}
}

func TestAssumeRoleConfigRejectsUninitializedClient(t *testing.T) {
	// No Init: DefaultConfig is zero. Building an STS client from it would
	// produce one that fails much later with an unrelated-looking error.
	client := newTestAWS(Config{Region: "eu-west-1"})

	_, err := client.AssumeRoleConfig(AssumeRoleOptions{
		RoleARN: "arn:aws:iam::222222222222:role/Sender",
	})
	if err == nil {
		t.Fatal("expected an error when Init has not run")
	}
	if !strings.Contains(err.Error(), "not initialized") {
		t.Errorf("error = %v, want it to name the real cause", err)
	}
}

func TestAssumeRoleConfigValidatesSessionName(t *testing.T) {
	client := initedTestAWS(t, Config{Region: "eu-west-1"})

	tests := []struct {
		name        string
		sessionName string
		wantErr     bool
	}{
		{"default when empty", "", false},
		{"tenant-shaped name", "notification-2acb1f32-7005-43f3-b280-d160985e1485", false},
		{"every allowed punctuation", "a_b+c=d,e.f@g-h", false},
		{"at the length limit", strings.Repeat("a", sessionNameMaxLen), false},
		{"one over the limit", strings.Repeat("a", sessionNameMaxLen+1), true},
		{"space", "tenant 1", true},
		{"slash", "tenant/1", true},
		{"colon", "arn:aws:iam::1:role/R", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.AssumeRoleConfig(AssumeRoleOptions{
				RoleARN:     "arn:aws:iam::222222222222:role/Sender",
				SessionName: tt.sessionName,
				Duration:    time.Hour,
			})
			if tt.wantErr && err == nil {
				t.Fatalf("SessionName %q was accepted; STS would reject it", tt.sessionName)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("SessionName %q: %v", tt.sessionName, err)
			}
		})
	}
}

func TestAssumeRoleConfigReusesOneSTSClient(t *testing.T) {
	client := initedTestAWS(t, Config{Region: "eu-west-1"})

	for range 3 {
		if _, err := client.AssumeRoleConfig(AssumeRoleOptions{
			RoleARN: "arn:aws:iam::222222222222:role/Sender",
		}); err != nil {
			t.Fatalf("AssumeRoleConfig: %v", err)
		}
	}

	// Roles are per tenant; the STS client that assumes them is not.
	if client.STSClient == nil {
		t.Fatal("STS client was not retained")
	}
	first := client.STSClient
	if _, err := client.AssumeRoleConfig(AssumeRoleOptions{
		RoleARN: "arn:aws:iam::333333333333:role/Other",
	}); err != nil {
		t.Fatalf("AssumeRoleConfig: %v", err)
	}
	if client.STSClient != first {
		t.Error("a second role rebuilt the STS client")
	}
}

func TestInitBuildsSTSClientWhenRequested(t *testing.T) {
	client := initedTestAWS(t, Config{Region: "eu-west-1"}, STS)

	if client.STSClient == nil {
		t.Fatal("STS was requested but no client was built")
	}
}

func TestDeriveBuildsOnlyTheRequestedClients(t *testing.T) {
	parent := initedTestAWS(t, Config{Region: "eu-west-1"}, S3, SQS, DynamoDB, SES)

	cfg, err := parent.AssumeRoleConfig(AssumeRoleOptions{
		RoleARN: "arn:aws:iam::222222222222:role/Sender",
		Region:  "us-east-2",
	})
	if err != nil {
		t.Fatalf("AssumeRoleConfig: %v", err)
	}

	derived, err := parent.Derive(cfg, SES)
	if err != nil {
		t.Fatalf("Derive: %v", err)
	}

	if derived.SESClient == nil {
		t.Error("SES was requested but no client was built")
	}
	// Handing a tenant's credentials to a DynamoDB client would point this
	// service's own storage at their account.
	if derived.DynamoDBClient != nil || derived.S3Client != nil || derived.SQSClient != nil {
		t.Error("Derive built a client that was not asked for")
	}
	if got := derived.SESClient.Options().Region; got != "us-east-2" {
		t.Errorf("derived SES region = %q, want the assumed role's %q", got, "us-east-2")
	}
	// The parent keeps serving this application's own account.
	if got := parent.SESClient.Options().Region; got != "eu-west-1" {
		t.Errorf("parent SES region = %q, want it untouched at %q", got, "eu-west-1")
	}
}

func TestDeriveInheritsSettingsButNotSharedState(t *testing.T) {
	const local = "http://localhost:4566"
	parent := initedTestAWS(t, Config{
		Region: "eu-west-1",
		SES:    SESConfig{EndpointURL: local, SenderEmail: "platform@example.com", SenderName: "Platform"},
	}, SES)

	derived, err := parent.Derive(parent.DefaultConfig, SES)
	if err != nil {
		t.Fatalf("Derive: %v", err)
	}

	// [aws] settings carry over, so a local stack stays reachable through a
	// derived client.
	if got := derived.SESClient.Options().BaseEndpoint; got == nil || *got != local {
		t.Errorf("derived ses endpoint = %v, want the inherited %q", got, local)
	}
	if derived.Logger != parent.Logger {
		t.Error("derived client lost the logger")
	}

	// Sending defaults are copied, so a per-tenant override cannot leak back
	// into the platform sender.
	derived.DefaultSES.SenderName = "Acme"
	if parent.DefaultSES.SenderName != "Platform" {
		t.Errorf("parent sender name = %q, want it untouched", parent.DefaultSES.SenderName)
	}

	// A derived client assumes further roles from its own credentials, so it
	// must not share the parent's STS client.
	if _, err := parent.AssumeRoleConfig(AssumeRoleOptions{RoleARN: "arn:aws:iam::1:role/R"}); err != nil {
		t.Fatalf("AssumeRoleConfig: %v", err)
	}
	if derived.STSClient != nil {
		t.Error("derived client shares the parent's STS client")
	}
}

func TestDeriveRejectsUnknownService(t *testing.T) {
	parent := initedTestAWS(t, Config{Region: "eu-west-1"})

	if _, err := parent.Derive(parent.DefaultConfig, Service("aws-lambda")); err == nil {
		t.Fatal("expected an error for a service the module does not know")
	}
}
