package settings

import "testing"

func TestFromEnvironmentDefaults(t *testing.T) {
	t.Setenv("AWS_DEFAULT_REGION", "")
	t.Setenv("REDIS_PORT", "")
	t.Setenv("ENCELADUS_REPORTS_BUCKET", "")
	t.Setenv("ENCELADUS_REPORTS_PREFIX", "")

	st := FromEnvironment()

	if st.AWSRegion != "eu-west-1" {
		t.Fatalf("unexpected default region: %q", st.AWSRegion)
	}
	if st.RedisPort != 6379 {
		t.Fatalf("unexpected default redis port: %d", st.RedisPort)
	}
	if st.ReportsBucket != "" {
		t.Fatalf("unexpected default bucket: %q", st.ReportsBucket)
	}
}

func TestFromEnvironmentOverrides(t *testing.T) {
	t.Setenv("AWS_DEFAULT_REGION", "us-east-1")
	t.Setenv("REDIS_PORT", "6380")
	t.Setenv("ENCELADUS_REPORTS_BUCKET", "enceladus-relatorios")
	t.Setenv("ENCELADUS_REPORTS_PREFIX", "prod/")

	st := FromEnvironment()

	if st.AWSRegion != "us-east-1" {
		t.Fatalf("unexpected region: %q", st.AWSRegion)
	}
	if st.RedisPort != 6380 {
		t.Fatalf("unexpected redis port: %d", st.RedisPort)
	}
	if st.ReportsBucket != "enceladus-relatorios" {
		t.Fatalf("unexpected bucket: %q", st.ReportsBucket)
	}
	if st.ReportsPrefix != "prod" {
		t.Fatalf("unexpected prefix: %q", st.ReportsPrefix)
	}
}
