package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// userDataSource exposes non-secret attributes of an existing database user.
type userDataSource struct {
	// dataSourceClient provides read-only catalog execution and binding checks.
	dataSourceClient
}

// userData contains user lookup input and observed capabilities and options. The collections are plain Go
// values, so a zero userData is a valid null configuration.
type userData struct {
	// ID is the paired resource's JSON identity.
	ID types.String `tfsdk:"id"`
	// Name identifies the requested database user.
	Name types.String `tfsdk:"name"`
	// Superuser reports the SQL CREATEUSER capability.
	Superuser types.Bool `tfsdk:"superuser"`
	// CreateDB reports permission to create databases.
	CreateDB types.Bool `tfsdk:"create_database"`
	// ValidUntil reports the password expiration.
	ValidUntil types.String `tfsdk:"valid_until"`
	// ConnectionLimit reports the connection limit; -1 stands for UNLIMITED.
	ConnectionLimit types.Int64 `tfsdk:"connection_limit"`
	// SessionTimeout reports the idle-session timeout; 0 means none.
	SessionTimeout types.Int64 `tfsdk:"session_timeout"`
	// SyslogAccess reports the system table visibility.
	SyslogAccess types.String `tfsdk:"syslog_access"`
	// ExternalID reports the identity-provider user identifier.
	ExternalID types.String `tfsdk:"external_id"`
	// SearchPath reports the stored search_path default.
	SearchPath []string `tfsdk:"search_path"`
	// SessionDefaults reports the other stored allowlisted session defaults.
	SessionDefaults map[string]string `tfsdk:"session_defaults"`
}

var _ = registerDataSource(newUserDataSource)

// newUserDataSource constructs a read-only SQL user lookup.
func newUserDataSource() datasource.DataSource { return &userDataSource{} }

// Metadata identifies the user data source to Terraform.
func (d *userDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user"
}

// Schema defines user lookup and non-secret attributes.
func (d *userDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a database user without exposing its password.",
		Attributes: map[string]schema.Attribute{
			"id":               dataSourceIDAttribute(),
			"name":             schema.StringAttribute{Required: true, MarkdownDescription: "Database user name; a missing user raises an error."},
			"superuser":        schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether the user has CREATEUSER."},
			"create_database":  schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether the user has CREATEDB."},
			"valid_until":      schema.StringAttribute{Computed: true, MarkdownDescription: "Password expiration as an RFC 3339 timestamp; null when the password does not expire."},
			"connection_limit": schema.Int64Attribute{Computed: true, MarkdownDescription: "Maximum concurrent connections; `-1` stands for `UNLIMITED`. Null when `SVV_USER_INFO` does not show the user, as for lookups by a regular user."},
			"session_timeout":  schema.Int64Attribute{Computed: true, MarkdownDescription: "Idle-session timeout in seconds; `0` when the user has none. Null when `SVV_USER_INFO` does not show the user."},
			"syslog_access":    schema.StringAttribute{Computed: true, MarkdownDescription: "`RESTRICTED` or `UNRESTRICTED`. Null when `SVV_USER_INFO` does not show the user."},
			"external_id":      schema.StringAttribute{Computed: true, MarkdownDescription: "Identity-provider user identifier; null when none is set or `SVV_USER_INFO` does not show the user."},
			"search_path":      schema.ListAttribute{Computed: true, ElementType: types.StringType, MarkdownDescription: "Stored `search_path` default in search order; null when none is stored."},
			"session_defaults": schema.MapAttribute{Computed: true, ElementType: types.StringType, MarkdownDescription: "Stored defaults of the session parameters `redshift_user` manages in `session_defaults`; null when none is stored."},
		},
	}
}

// Read resolves user capabilities and options without reading or changing credentials.
func (d *userDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data userData
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	user := userNullModel(data.Name)
	found, err := (&userResource{d.resourceClient}).read(ctx, &user)
	if err != nil {
		resp.Diagnostics.AddError("Read Redshift user", err.Error())
		return
	}
	if !found {
		resp.Diagnostics.AddError("User not found", "No database user named "+data.Name.ValueString()+" exists.")
		return
	}
	data.Name, data.Superuser, data.CreateDB, data.ValidUntil = user.Name, user.Superuser, user.CreateDB, user.ValidUntil
	// The resource records no expiration as its infinity default; the lookup keeps reporting it as null.
	if data.ValidUntil.ValueString() == userValidUntilInfinity {
		data.ValidUntil = types.StringNull()
	}
	data.ConnectionLimit, data.SessionTimeout = user.ConnectionLimit, user.SessionTimeout
	data.SyslogAccess, data.ExternalID = user.SyslogAccess, user.ExternalID
	data.SearchPath = userListStrings(user.SearchPath)
	data.SessionDefaults = knownMap(user.SessionDefaults)
	data.ID = d.identity(d.database.ValueString(), map[string]string{"name": data.Name.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
