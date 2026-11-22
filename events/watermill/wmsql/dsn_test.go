package dogerwmsql_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDSNRequired(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		ci       string
		goos     string
		wantSkip string
		wantFail string
	}{
		{name: "ci_unset", goos: "linux", wantSkip: "set DOGER_TEST_POSTGRES_DSN to run"},
		{name: "ci_true", ci: "true", goos: "linux", wantFail: "DOGER_TEST_POSTGRES_DSN is required in CI"},
		{name: "ci_true_darwin", ci: "true", goos: "darwin", wantSkip: "set DOGER_TEST_POSTGRES_DSN to run"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := map[string]string{"CI": tt.ci}

			dsn, skip, fail := lookupDSN(envPostgresDSN, func(k string) string { return env[k] }, tt.goos)

			assert.Empty(t, dsn)
			assert.Equal(t, tt.wantSkip, skip)
			assert.Equal(t, tt.wantFail, fail)
		})
	}
}
