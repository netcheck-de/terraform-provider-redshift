package redshiftconn

import (
	"cmp"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// credentialFunc supplies controlled temporary credentials for direct-client tests.
type credentialFunc func(context.Context, string) (Credentials, error)

// Resolve adapts a callback to the database-specific credential contract.
func (f credentialFunc) Resolve(ctx context.Context, database string) (Credentials, error) {
	return f(ctx, database)
}

// rowsStub implements only the row operations exercised by the direct SQL adapter.
type rowsStub struct {
	// Rows supplies unused pgx methods without duplicating its complete interface.
	pgx.Rows
	// values contains the single result row used in a test.
	values []any
	// columns provides the corresponding result column names.
	columns []pgconn.FieldDescription
	// valueError simulates row decoding failures.
	valueError error
	// resultError simulates a server error observed at query completion.
	resultError error
	// consumed records whether the single row has been returned.
	consumed bool
	// closed records deterministic result cleanup.
	closed bool
}

// Next returns the configured row once, or no rows for a command-only statement.
func (r *rowsStub) Next() bool {
	if r.consumed || r.values == nil {
		return false
	}
	r.consumed = true
	return true
}

// Values returns the configured row or decoding failure.
func (r *rowsStub) Values() ([]any, error) { return r.values, r.valueError }

// FieldDescriptions supplies result column metadata.
func (r *rowsStub) FieldDescriptions() []pgconn.FieldDescription { return r.columns }

// Err returns the configured completion error.
func (r *rowsStub) Err() error { return r.resultError }

// Close records cleanup of the result stream.
func (r *rowsStub) Close() { r.closed = true }

// connectionStub records statement execution and session cleanup without opening a socket.
type connectionStub struct {
	// rows supplies the result stream returned by Query.
	rows *rowsStub
	// queryError simulates statement submission failures.
	queryError error
	// statement records the safely rebound SQL.
	statement string
	// arguments records parameter values independently of SQL text.
	arguments []any
	// closed records session cleanup even when the query context has expired.
	closed bool
}

// Query records execution inputs and returns the configured result.
func (c *connectionStub) Query(_ context.Context, statement string, arguments ...any) (pgx.Rows, error) {
	c.statement, c.arguments = statement, arguments
	return c.rows, c.queryError
}

// Close records session cleanup using a fresh context.
func (c *connectionStub) Close(context.Context) error { c.closed = true; return nil }

// directTestClient supplies deterministic credentials and a bounded query timeout.
func directTestClient() *Client {
	return &Client{Credentials: Credentials{Host: "warehouse.example.com", Username: "reader", Password: "secret"}, Timeout: time.Second}
}

// TestDirectQueryBindsRowsAndCloses verifies uncached parameterized execution and textual result conversion.
func TestDirectQueryBindsRowsAndCloses(t *testing.T) {
	client := directTestClient()
	rows := &rowsStub{values: []any{"value", true, nil, int64(42), []byte("text")}, columns: []pgconn.FieldDescription{{Name: "value"}, {Name: "enabled"}, {Name: "null"}, {Name: "count"}, {Name: "bytes"}}}
	session := &connectionStub{rows: rows}
	client.dial = func(_ context.Context, config *pgx.ConnConfig) (connection, error) {
		assert.Equal(t, "analytics", config.Database)
		assert.Equal(t, "warehouse.example.com", config.TLSConfig.ServerName)
		assert.False(t, config.TLSConfig.InsecureSkipVerify)
		assert.Equal(t, uint16(tls.VersionTLS12), config.TLSConfig.MinVersion)
		assert.Empty(t, config.Fallbacks)
		assert.Equal(t, pgx.QueryExecModeExec, config.DefaultQueryExecMode)
		return session, nil
	}
	result, err := client.Query(context.Background(), sqlclient.Connection{Database: "analytics"}, "SELECT :name", map[string]string{"name": "O'Reilly"})
	require.NoError(t, err)
	assert.Equal(t, "SELECT $1", session.statement)
	assert.Equal(t, []any{"O'Reilly"}, session.arguments)
	assert.Equal(t, []sqlclient.Row{{"value": "value", "enabled": "true", "null": "", "count": "42", "bytes": "text"}}, result)
	assert.True(t, session.closed)
	assert.True(t, rows.closed)
}

// TestDirectQueryFailureCleanup checks binding, dialing, execution, decoding, metadata, and completion failures.
func TestDirectQueryFailureCleanup(t *testing.T) {
	for _, mode := range []string{"bind", "configuration", "dial", "query", "values", "metadata", "unsupported", "completion", "command", "default dial"} {
		t.Run(mode, func(t *testing.T) {
			client := directTestClient()
			rows := &rowsStub{values: []any{"value"}, columns: []pgconn.FieldDescription{{Name: "value"}}}
			session := &connectionStub{rows: rows}
			client.dial = func(context.Context, *pgx.ConnConfig) (connection, error) {
				if mode == "dial" {
					return nil, errors.New("dial failed")
				}
				return session, nil
			}
			sql := "SELECT 1"
			switch mode {
			case "bind":
				sql = "SELECT :missing"
			case "configuration":
				client.Credentials.Host = ""
			case "query":
				session.queryError = errors.New("query failed")
			case "values":
				rows.valueError = errors.New("decode failed")
			case "metadata":
				rows.columns = nil
			case "unsupported":
				rows.values = []any{struct{}{}}
			case "completion":
				rows.resultError = errors.New("server error")
			case "command":
				rows.values = nil
			case "default dial":
				client.dial = nil
				client.Credentials.Host = "127.0.0.1"
				client.Credentials.Port = 1
			}
			_, err := client.Query(context.Background(), sqlclient.Connection{Database: "analytics"}, sql, nil)
			if mode == "command" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			if mode == "query" || mode == "values" || mode == "metadata" || mode == "unsupported" || mode == "completion" || mode == "command" {
				assert.True(t, session.closed)
			}
		})
	}
}

// TestDirectConfigurationUsesFreshIAMAndOverrides checks database-specific authentication and custom endpoint routing.
func TestDirectConfigurationUsesFreshIAMAndOverrides(t *testing.T) {
	client := directTestClient()
	client.Credentials.Port = 5440
	client.IAM = credentialFunc(func(_ context.Context, database string) (Credentials, error) {
		assert.Equal(t, "analytics", database)
		return Credentials{Host: "discovered.example.com", Port: 5439, Username: "IAM:reader", Password: "temporary"}, nil
	})
	config, err := client.configuration(context.Background(), "analytics")
	require.NoError(t, err)
	assert.Equal(t, "warehouse.example.com", config.Host)
	assert.Equal(t, uint16(5440), config.Port)
	assert.Equal(t, "IAM:reader", config.User)
	assert.Equal(t, "temporary", config.Password)
	client.Credentials = Credentials{}
	config, err = client.configuration(context.Background(), "analytics")
	require.NoError(t, err)
	assert.Equal(t, "discovered.example.com", config.Host)
	client.IAM = credentialFunc(func(context.Context, string) (Credentials, error) {
		return Credentials{}, errors.New("credentials failed")
	})
	_, err = client.configuration(context.Background(), "analytics")
	require.Error(t, err)
}

// TestDirectConfigurationCAValidation checks explicit CA trust, invalid PEM, missing files, and platform failures.
func TestDirectConfigurationCAValidation(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	certificate := &x509.Certificate{SerialNumber: big.NewInt(1), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600))
	client := directTestClient()
	client.CACertFile = path
	config, err := client.configuration(context.Background(), "analytics")
	require.NoError(t, err)
	assert.NotNil(t, config.TLSConfig.RootCAs)
	require.NoError(t, os.WriteFile(path, []byte("invalid"), 0o600))
	_, err = client.configuration(context.Background(), "analytics")
	require.ErrorContains(t, err, "valid certificates")
	client.CACertFile = filepath.Join(t.TempDir(), "absent")
	_, err = client.configuration(context.Background(), "analytics")
	require.ErrorContains(t, err, "CA bundle")
	client.CACertFile = path
	original := systemCertPool
	t.Cleanup(func() { systemCertPool = original })
	systemCertPool = func() (*x509.CertPool, error) { return nil, errors.New("trust failed") }
	_, err = client.configuration(context.Background(), "analytics")
	require.ErrorContains(t, err, "system CA trust")
}

// TestDirectScalarText preserves numeric and UTC timestamp text and rejects unsupported values.
func TestDirectScalarText(t *testing.T) {
	for _, value := range []any{int16(2), int32(3), int(4), float32(1.5), float64(2.5), time.Date(2026, 1, 1, 1, 0, 0, 0, time.FixedZone("offset", 3600))} {
		_, err := valueText(value)
		require.NoError(t, err)
	}
	text, err := valueText(time.Date(2026, 1, 1, 1, 0, 0, 0, time.FixedZone("offset", 3600)))
	require.NoError(t, err)
	assert.Equal(t, "2026-01-01T00:00:00Z", text)
}

// TestDirectConfigurationRejectsInvalidPGEnvironment checks configuration errors without attempting a network connection.
func TestDirectConfigurationRejectsInvalidPGEnvironment(t *testing.T) {
	t.Setenv("PGPORT", "invalid")
	_, err := directTestClient().configuration(context.Background(), "analytics")
	require.ErrorContains(t, err, "parse SQL configuration")
}

// TestDirectConfigurationConnectTimeoutAndApplicationName keeps the connect timeout and application name independent
// of the query timeout and of PG* environment variables, including malformed ones.
func TestDirectConfigurationConnectTimeoutAndApplicationName(t *testing.T) {
	for _, value := range []string{"1", "invalid"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("PGCONNECT_TIMEOUT", value)
			t.Setenv("PGAPPNAME", "environment")
			client := directTestClient()
			client.Timeout = time.Hour
			config, err := client.configuration(context.Background(), "analytics")
			require.NoError(t, err)
			assert.Equal(t, DefaultConnectTimeout, config.ConnectTimeout)
			assert.NotContains(t, config.RuntimeParams, "application_name")
			client.ConnectTimeout, client.ApplicationName = 10*time.Second, "terraform-provider-redshift/test"
			config, err = client.configuration(context.Background(), "analytics")
			require.NoError(t, err)
			assert.Equal(t, 10*time.Second, config.ConnectTimeout)
			assert.Equal(t, map[string]string{"client_encoding": "UTF8", "application_name": "terraform-provider-redshift/test"}, config.RuntimeParams)
		})
	}
}

// TestDirectQueryTimeoutCoversCredentials bounds IAM credential lookups by the query timeout, not the connect timeout,
// and lets an earlier caller deadline win.
func TestDirectQueryTimeoutCoversCredentials(t *testing.T) {
	for _, callerDeadline := range []bool{false, true} {
		var remaining time.Duration
		client := directTestClient()
		client.Timeout, client.ConnectTimeout = time.Hour, time.Second
		client.IAM = credentialFunc(func(ctx context.Context, _ string) (Credentials, error) {
			deadline, ok := ctx.Deadline()
			require.True(t, ok)
			remaining = time.Until(deadline)
			return Credentials{Host: "warehouse.example.com", Username: "IAM:reader", Password: "temporary"}, nil
		})
		client.dial = func(context.Context, *pgx.ConnConfig) (connection, error) {
			return &connectionStub{rows: &rowsStub{}}, nil
		}
		ctx := context.Background()
		if callerDeadline {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, time.Minute)
			defer cancel()
		}
		_, err := client.Query(ctx, sqlclient.Connection{Database: "analytics"}, "SELECT 1", nil)
		require.NoError(t, err)
		if callerDeadline {
			assert.LessOrEqual(t, remaining, time.Minute)
		} else {
			assert.Greater(t, remaining, 59*time.Minute)
		}
	}
}

// TestDirectConfigurationSSLModes maps each sslmode to its TLS behavior and verifies chains without hostnames for verify-ca.
func TestDirectConfigurationSSLModes(t *testing.T) {
	for mode, check := range map[string]func(*tls.Config){
		"": func(config *tls.Config) {
			assert.False(t, config.InsecureSkipVerify)
			assert.Equal(t, "warehouse.example.com", config.ServerName)
		},
		SSLModeVerifyFull: func(config *tls.Config) { assert.False(t, config.InsecureSkipVerify) },
		SSLModeVerifyCA: func(config *tls.Config) {
			assert.True(t, config.InsecureSkipVerify)
			require.NotNil(t, config.VerifyConnection)
		},
		SSLModeRequire: func(config *tls.Config) {
			assert.True(t, config.InsecureSkipVerify)
			assert.Nil(t, config.VerifyConnection)
		},
		SSLModeDisable: func(config *tls.Config) { assert.Nil(t, config) },
	} {
		t.Run(cmp.Or(mode, "default"), func(t *testing.T) {
			client := directTestClient()
			client.SSLMode = mode
			config, err := client.configuration(context.Background(), "analytics")
			require.NoError(t, err)
			check(config.TLSConfig)
			assert.Empty(t, config.Fallbacks)
		})
	}
	client := directTestClient()
	client.SSLMode = "prefer"
	_, err := client.configuration(context.Background(), "analytics")
	require.ErrorContains(t, err, "unsupported sslmode")

	// verify-ca accepts a chain to a trusted root regardless of hostname and rejects untrusted or missing certificates.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	root := &x509.Certificate{SerialNumber: big.NewInt(1), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign, NotAfter: time.Now().Add(time.Hour)}
	rootDER, err := x509.CreateCertificate(rand.Reader, root, root, &key.PublicKey, key)
	require.NoError(t, err)
	rootCertificate, err := x509.ParseCertificate(rootDER)
	require.NoError(t, err)
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: []string{"other.example.com"}, NotAfter: time.Now().Add(time.Hour), ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, rootCertificate, &key.PublicKey, key)
	require.NoError(t, err)
	leafCertificate, err := x509.ParseCertificate(leafDER)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rootDER}), 0o600))
	client = directTestClient()
	client.SSLMode, client.CACertFile = SSLModeVerifyCA, path
	config, err := client.configuration(context.Background(), "analytics")
	require.NoError(t, err)
	verify := config.TLSConfig.VerifyConnection
	require.NoError(t, verify(tls.ConnectionState{PeerCertificates: []*x509.Certificate{leafCertificate, rootCertificate}}))
	require.ErrorContains(t, verify(tls.ConnectionState{}), "no certificate")
	client.CACertFile = ""
	config, err = client.configuration(context.Background(), "analytics")
	require.NoError(t, err)
	require.Error(t, config.TLSConfig.VerifyConnection(tls.ConnectionState{PeerCertificates: []*x509.Certificate{leafCertificate}}))
}
