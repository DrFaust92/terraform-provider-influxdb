package influxdb

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/resourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/influxdata/influxdb/client"
)

var (
	_ resource.Resource                     = &userResource{}
	_ resource.ResourceWithConfigure        = &userResource{}
	_ resource.ResourceWithImportState      = &userResource{}
	_ resource.ResourceWithConfigValidators = &userResource{}
)

func newUserResource() resource.Resource {
	return &userResource{}
}

type userResource struct {
	conn *client.Client
}

type userModel struct {
	ID                types.String `tfsdk:"id"`
	Name              types.String `tfsdk:"name"`
	Password          types.String `tfsdk:"password"`
	PasswordWO        types.String `tfsdk:"password_wo"`
	PasswordWOVersion types.String `tfsdk:"password_wo_version"`
	Admin             types.Bool   `tfsdk:"admin"`
	Grants            []grantModel `tfsdk:"grant"`
}

type grantModel struct {
	Database  types.String `tfsdk:"database"`
	Privilege types.String `tfsdk:"privilege"`
}

func (r *userResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user"
}

func (r *userResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages an InfluxDB user and its database privileges.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "The user name.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:      true,
				Description:   "Name of the user.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"password": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Password of the user. Changing it re-creates the user; use `password_wo` to rotate the password in place. Exactly one of `password` and `password_wo` must be set.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplaceIf(passwordRequiresReplace,
						"Changing the password re-creates the user.",
						"Changing the password re-creates the user."),
				},
			},
			"password_wo": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				WriteOnly:   true,
				Description: "Write-only alternative to `password`, never stored in state. Requires Terraform 1.11 or later.",
				Validators: []validator.String{
					stringvalidator.ConflictsWith(path.MatchRoot("password")),
					stringvalidator.AlsoRequires(path.MatchRoot("password_wo_version")),
				},
			},
			"password_wo_version": schema.StringAttribute{
				Optional:    true,
				Description: "Change this value to update `password_wo`, Terraform cannot detect changes to write-only attributes.",
				Validators: []validator.String{
					stringvalidator.AlsoRequires(path.MatchRoot("password_wo")),
				},
			},
			"admin": schema.BoolAttribute{
				Optional:      true,
				Computed:      true,
				Description:   "Whether the user has all privileges (is an admin).",
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
		},
		Blocks: map[string]schema.Block{
			"grant": schema.SetNestedBlock{
				Description: "Privileges of the user on databases.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"database": schema.StringAttribute{
							Required:    true,
							Description: "Database the privilege applies to.",
						},
						"privilege": schema.StringAttribute{
							Required:    true,
							Description: "One of `READ`, `WRITE` or `ALL`.",
							Validators:  []validator.String{stringvalidator.OneOf("READ", "WRITE", "ALL")},
						},
					},
				},
			},
		},
	}
}

func (r *userResource) ConfigValidators(_ context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{
		resourcevalidator.ExactlyOneOf(path.MatchRoot("password"), path.MatchRoot("password_wo")),
	}
}

// passwordRequiresReplace replaces the user when the password changes. State
// written by provider versions before 2.0 holds hashSum(password) instead of
// the password, which is not a change: the next apply only stores the
// password in state, and so does state from import, which has no password.
func passwordRequiresReplace(_ context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
	if req.StateValue.IsNull() || req.PlanValue.IsUnknown() || req.PlanValue.IsNull() {
		return
	}
	prior := req.StateValue.ValueString()
	planned := req.PlanValue.ValueString()
	resp.RequiresReplace = prior != planned && prior != hashSum(planned)
}

func (r *userResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	conn, err := clientFromProviderData(req.ProviderData)
	if err != nil {
		resp.Diagnostics.AddError("Unexpected provider data", err.Error())
		return
	}
	r.conn = conn
}

func (r *userResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan, config userModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := plan.Name.ValueString()
	password := plan.Password.ValueString()
	// Write-only values are only available from the configuration.
	if !config.PasswordWO.IsNull() {
		password = config.PasswordWO.ValueString()
	}

	adminPrivileges := ""
	if plan.Admin.ValueBool() {
		adminPrivileges = " WITH ALL PRIVILEGES"
	}

	if err := exec(r.conn, fmt.Sprintf("CREATE USER %q WITH PASSWORD %s%s", name, quoteString(password), adminPrivileges)); err != nil {
		resp.Diagnostics.AddError("Unable to create user", err.Error())
		return
	}

	for _, g := range plan.Grants {
		if err := grantPrivilegeOn(r.conn, g.Privilege.ValueString(), g.Database.ValueString(), name); err != nil {
			resp.Diagnostics.AddError("Unable to grant privilege", err.Error())
			return
		}
	}

	plan.ID = types.StringValue(name)
	plan.PasswordWO = types.StringNull()
	if found := r.read(&plan, &resp.Diagnostics); !found && !resp.Diagnostics.HasError() {
		resp.Diagnostics.AddError("User not found", fmt.Sprintf("user %q was not found after creating it", name))
	}
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *userResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state userModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	found := r.read(&state, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *userResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state, config userModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := state.ID.ValueString()

	// Changing password_wo_version updates the password in place. A change of
	// password only reaches Update when the prior state held its hash or
	// nothing (see passwordRequiresReplace), so only state is updated then.
	if !plan.PasswordWOVersion.Equal(state.PasswordWOVersion) {
		password := plan.Password.ValueString()
		if !config.PasswordWO.IsNull() {
			password = config.PasswordWO.ValueString()
		}
		if err := exec(r.conn, fmt.Sprintf("SET PASSWORD FOR %q = %s", name, quoteString(password))); err != nil {
			resp.Diagnostics.AddError("Unable to set password", err.Error())
			return
		}
	}

	if !plan.Admin.IsUnknown() && !plan.Admin.Equal(state.Admin) {
		var err error
		if plan.Admin.ValueBool() {
			err = exec(r.conn, fmt.Sprintf("GRANT ALL PRIVILEGES TO %q", name))
		} else {
			err = exec(r.conn, fmt.Sprintf("REVOKE ALL PRIVILEGES FROM %q", name))
		}
		if err != nil {
			resp.Diagnostics.AddError("Unable to change admin privileges", err.Error())
			return
		}
	}

	if err := updateGrants(r.conn, name, state.Grants, plan.Grants); err != nil {
		resp.Diagnostics.AddError("Unable to update grants", err.Error())
		return
	}

	plan.ID = types.StringValue(name)
	plan.PasswordWO = types.StringNull()
	if found := r.read(&plan, &resp.Diagnostics); !found && !resp.Diagnostics.HasError() {
		resp.Diagnostics.AddError("User not found", fmt.Sprintf("user %q was not found after updating it", name))
	}
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *userResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state userModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := exec(r.conn, fmt.Sprintf("DROP USER %q", state.ID.ValueString())); err != nil {
		resp.Diagnostics.AddError("Unable to drop user", err.Error())
	}
}

func (r *userResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("grant"), []grantModel{})...)
}

// read refreshes the admin flag and grants of the user in m from the server.
// It reports whether the user exists.
func (r *userResource) read(m *userModel, diags interface{ AddError(string, string) }) bool {
	name := m.ID.ValueString()

	// InfluxDB has no command to check for a single user, so the list of all
	// users is searched.
	resp, err := queryResponse(r.conn, "SHOW USERS")
	if err != nil {
		diags.AddError("Unable to read users", err.Error())
		return false
	}

	found := false
	for _, row := range firstSeriesValues(resp) {
		if row[0] == name {
			found = true
			admin, _ := row[1].(bool)
			m.Name = types.StringValue(name)
			m.Admin = types.BoolValue(admin)
			break
		}
	}
	if !found {
		return false
	}

	grants, err := readGrants(r.conn, name)
	if err != nil {
		diags.AddError("Unable to read grants", err.Error())
		return false
	}
	m.Grants = grants
	return true
}

func readGrants(conn *client.Client, name string) ([]grantModel, error) {
	resp, err := queryResponse(conn, fmt.Sprintf("SHOW GRANTS FOR %q", name))
	if err != nil {
		return nil, err
	}

	grants := []grantModel{}
	for _, row := range firstSeriesValues(resp) {
		privilege, _ := row[1].(string)
		if privilege == "NO PRIVILEGES" {
			continue
		}
		database, _ := row[0].(string)
		grants = append(grants, grantModel{
			Database:  types.StringValue(database),
			Privilege: types.StringValue(strings.Replace(strings.ToUpper(privilege), "ALL PRIVILEGES", "ALL", 1)),
		})
	}
	return grants, nil
}

// updateGrants revokes, changes and adds grants so the user has the planned ones.
func updateGrants(conn *client.Client, user string, old, planned []grantModel) error {
	plannedByDB := map[string]string{}
	for _, g := range planned {
		plannedByDB[g.Database.ValueString()] = g.Privilege.ValueString()
	}
	oldByDB := map[string]string{}
	for _, g := range old {
		oldByDB[g.Database.ValueString()] = g.Privilege.ValueString()
	}

	for db, privilege := range oldByDB {
		newPrivilege, ok := plannedByDB[db]
		if !ok {
			if err := exec(conn, fmt.Sprintf("REVOKE %s ON %q FROM %q", privilege, db, user)); err != nil {
				return err
			}
		} else if newPrivilege != privilege {
			// GRANT adds to the existing privilege (READ plus WRITE is ALL), so
			// the old one is revoked first.
			if err := exec(conn, fmt.Sprintf("REVOKE %s ON %q FROM %q", privilege, db, user)); err != nil {
				return err
			}
			if err := grantPrivilegeOn(conn, newPrivilege, db, user); err != nil {
				return err
			}
		}
	}

	for db, privilege := range plannedByDB {
		if _, ok := oldByDB[db]; !ok {
			if err := grantPrivilegeOn(conn, privilege, db, user); err != nil {
				return err
			}
		}
	}
	return nil
}

func grantPrivilegeOn(conn *client.Client, privilege, database, user string) error {
	return exec(conn, fmt.Sprintf("GRANT %s ON %q TO %q", privilege, database, user))
}
