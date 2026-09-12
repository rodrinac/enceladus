package settings

import "testing"

func TestFromEnvironmentDefaults(t *testing.T) {
	t.Setenv("SES_SENDER", "")
	t.Setenv("SES_CONFIGURATION_SET", "")
	t.Setenv("AWS_DEFAULT_REGION", "")
	t.Setenv("REDIS_PORT", "")

	st := FromEnvironment()

	if st.SESSender != "Enceladus Big Data <enceladus.bigdata@hotmail.com>" {
		t.Fatalf("unexpected default sender: %q", st.SESSender)
	}
	if st.SESConfigurationSet != "Default" {
		t.Fatalf("unexpected default configuration set: %q", st.SESConfigurationSet)
	}
	if st.SESRegion != "eu-west-1" {
		t.Fatalf("unexpected default SES region: %q", st.SESRegion)
	}
	if st.RedisPort != 6379 {
		t.Fatalf("unexpected default redis port: %d", st.RedisPort)
	}
}

func TestFromEnvironmentOverrides(t *testing.T) {
	t.Setenv("SES_SENDER", "ops@example.com")
	t.Setenv("SES_CONFIGURATION_SET", "Relatorios")
	t.Setenv("AWS_DEFAULT_REGION", "us-east-1")
	t.Setenv("REDIS_PORT", "6380")

	st := FromEnvironment()

	if st.SESSender != "ops@example.com" {
		t.Fatalf("unexpected sender: %q", st.SESSender)
	}
	if st.SESConfigurationSet != "Relatorios" {
		t.Fatalf("unexpected configuration set: %q", st.SESConfigurationSet)
	}
	if st.SESRegion != "us-east-1" {
		t.Fatalf("unexpected SES region: %q", st.SESRegion)
	}
	if st.RedisPort != 6380 {
		t.Fatalf("unexpected redis port: %d", st.RedisPort)
	}
}