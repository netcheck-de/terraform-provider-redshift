package redshiftconn

import (
	"cmp"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"slices"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// Credentials describes endpoint routing and authentication for one new SQL connection.
type Credentials struct {
	// Host is the verified TLS endpoint hostname.
	Host string
	// Port is the endpoint TCP port.
	Port uint16
	// Username is the existing or AWS-derived SQL user.
	Username string
	// Password is the static or temporary authentication secret, retained only in memory.
	Password string
}

// CredentialProvider resolves database-specific temporary credentials without persisting them in Terraform state.
type CredentialProvider interface {
	// Resolve obtains credentials valid for opening a connection to this database.
	Resolve(context.Context, string) (Credentials, error)
}

// connection is the pgx surface needed for deterministic per-statement connection lifetime.
type connection interface {
	// Query executes autocommit SQL and returns its rows or command completion.
	Query(context.Context, string, ...any) (pgx.Rows, error)
	// Close releases the SQL session independently of the caller's cancellation.
	Close(context.Context) error
}

// DefaultTimeout bounds a query when Client.Timeout is unset.
const DefaultTimeout = 5 * time.Minute

// Supported TLS modes, matching the libpq sslmode names. Fallback modes such as prefer are deliberately unsupported.
const (
	// SSLModeVerifyFull verifies the certificate chain and the hostname.
	SSLModeVerifyFull = "verify-full"
	// SSLModeVerifyCA verifies the certificate chain but not the hostname.
	SSLModeVerifyCA = "verify-ca"
	// SSLModeRequire encrypts without verifying the server certificate.
	SSLModeRequire = "require"
	// SSLModeDisable connects without TLS.
	SSLModeDisable = "disable"
)

// SSLModes lists every supported sslmode value.
var SSLModes = []string{SSLModeVerifyFull, SSLModeVerifyCA, SSLModeRequire, SSLModeDisable}

// Client opens and closes one TLS SQL session per query; it never retains a pool without a lifecycle close hook.
type Client struct {
	// Credentials supplies static routing/authentication, including optional IAM endpoint overrides.
	Credentials Credentials
	// IAM supplies fresh database-specific temporary credentials when configured.
	IAM CredentialProvider
	// CACertFile augments system certificate trust with a PEM bundle.
	CACertFile string
	// SSLMode selects TLS verification: SSLModeVerifyFull (the default when empty), SSLModeVerifyCA, SSLModeRequire,
	// or SSLModeDisable.
	SSLMode string
	// Timeout bounds credential acquisition, connection establishment, and SQL execution; zero selects DefaultTimeout.
	Timeout time.Duration
	// dial permits deterministic protocol/error testing without a real warehouse.
	dial func(context.Context, *pgx.ConnConfig) (connection, error)
}

// Verify the direct adapter satisfies the resource-facing SQL contract.
var _ sqlclient.Client = (*Client)(nil)

// systemCertPool is replaceable in tests to cover platform trust-store failures.
var systemCertPool = x509.SystemCertPool

// valueText converts SQL scalars into the same textual/null representation used by the Data API adapter.
func valueText(value any) (string, error) {
	switch value := value.(type) {
	case nil:
		return "", nil
	case string:
		return value, nil
	case []byte:
		return string(value), nil
	case bool:
		return strconv.FormatBool(value), nil
	case int16, int32, int64, int, float32, float64:
		return fmt.Sprint(value), nil
	case time.Time:
		return value.UTC().Format(time.RFC3339Nano), nil
	default:
		return "", fmt.Errorf("unsupported SQL result value %T", value)
	}
}

// configuration constructs routing and TLS for the selected sslmode without connecting or consulting PostgreSQL
// credential files.
func (c *Client) configuration(ctx context.Context, database string) (*pgx.ConnConfig, error) {
	credentials := c.Credentials
	if c.IAM != nil {
		resolved, err := c.IAM.Resolve(ctx, database)
		if err != nil {
			return nil, err
		}
		if credentials.Host != "" {
			resolved.Host = credentials.Host
		}
		if credentials.Port != 0 {
			resolved.Port = credentials.Port
		}
		credentials = resolved
	}
	if credentials.Host == "" || credentials.Username == "" || credentials.Password == "" || database == "" {
		return nil, fmt.Errorf("direct SQL host, username, password, and database must be known and nonempty")
	}
	if credentials.Port == 0 {
		credentials.Port = 5439
	}
	mode := cmp.Or(c.SSLMode, SSLModeVerifyFull)
	if !slices.Contains(SSLModes, mode) {
		return nil, fmt.Errorf("unsupported sslmode %q", mode)
	}
	// pgx requires a parsed config; explicitly overwrite environment/service/passfile-derived routing and authentication.
	config, err := pgx.ParseConfig("sslmode=" + mode)
	if err != nil {
		return nil, fmt.Errorf("parse SQL configuration: %w", err)
	}
	config.Host, config.Port, config.User, config.Password, config.Database = credentials.Host, credentials.Port, credentials.Username, credentials.Password, database
	config.Fallbacks = nil
	config.ConnectTimeout = cmp.Or(c.Timeout, DefaultTimeout)
	config.RuntimeParams = map[string]string{"client_encoding": "UTF8"}
	// Exec uses safe wire-protocol parameter binding without prepared-statement caching or PostgreSQL-only startup flags.
	config.DefaultQueryExecMode = pgx.QueryExecModeExec
	config.TLSConfig, err = c.tlsConfig(mode, credentials.Host)
	if err != nil {
		return nil, err
	}
	return config, nil
}

// tlsConfig builds the TLS settings for one sslmode; nil disables TLS.
func (c *Client) tlsConfig(mode, host string) (*tls.Config, error) {
	switch mode {
	case SSLModeDisable:
		return nil, nil
	case SSLModeRequire:
		return &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host, InsecureSkipVerify: true}, nil //nolint:gosec // Explicit user opt-in.
	}
	roots, err := c.roots()
	if err != nil {
		return nil, err
	}
	config := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host, RootCAs: roots}
	if mode == SSLModeVerifyCA {
		// Go cannot verify a chain without a hostname directly; skip the built-in check and verify the chain manually.
		config.InsecureSkipVerify = true //nolint:gosec // The chain is verified in VerifyConnection.
		config.VerifyConnection = func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) == 0 {
				return fmt.Errorf("server presented no certificate")
			}
			intermediates := x509.NewCertPool()
			for _, certificate := range state.PeerCertificates[1:] {
				intermediates.AddCert(certificate)
			}
			_, err := state.PeerCertificates[0].Verify(x509.VerifyOptions{Roots: roots, Intermediates: intermediates})
			return err
		}
	}
	return config, nil
}

// roots returns nil for system trust, or the system pool extended with CACertFile.
func (c *Client) roots() (*x509.CertPool, error) {
	if c.CACertFile == "" {
		return nil, nil
	}
	pem, err := os.ReadFile(c.CACertFile)
	if err != nil {
		return nil, fmt.Errorf("read SQL CA bundle: %w", err)
	}
	roots, err := systemCertPool()
	if err != nil {
		return nil, fmt.Errorf("load system CA trust: %w", err)
	}
	if !roots.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("SQL CA bundle contains no valid certificates")
	}
	return roots, nil
}

// Query executes one autocommit statement with safe positional binding and closes the connection deterministically.
func (c *Client) Query(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
	ctx, cancel := context.WithTimeout(ctx, cmp.Or(c.Timeout, DefaultTimeout))
	defer cancel()
	statement, arguments, err := bindParameters(sql, parameters)
	if err != nil {
		return nil, err
	}
	config, err := c.configuration(ctx, target.Database)
	if err != nil {
		return nil, err
	}
	dial := c.dial
	if dial == nil {
		dial = func(ctx context.Context, config *pgx.ConnConfig) (connection, error) {
			return pgx.ConnectConfig(ctx, config)
		}
	}
	session, err := dial(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("connect to Redshift SQL endpoint: %w", err)
	}
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		_ = session.Close(closeCtx)
	}()
	rows, err := session.Query(ctx, statement, arguments...)
	if err != nil {
		return nil, fmt.Errorf("execute direct SQL: %w", err)
	}
	defer rows.Close()
	var result []sqlclient.Row
	columns := rows.FieldDescriptions()
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, fmt.Errorf("read direct SQL row: %w", err)
		}
		if len(values) != len(columns) {
			return nil, fmt.Errorf("direct SQL returned inconsistent column metadata")
		}
		row := sqlclient.Row{}
		for index, value := range values {
			text, err := valueText(value)
			if err != nil {
				return nil, err
			}
			row[columns[index].Name] = text
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("finish direct SQL: %w", err)
	}
	return result, nil
}
