package influxdb

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/influxdata/influxdb/client"
)

var (
	_ resource.Resource                = &databaseResource{}
	_ resource.ResourceWithConfigure   = &databaseResource{}
	_ resource.ResourceWithImportState = &databaseResource{}
	_ resource.ResourceWithModifyPlan  = &databaseResource{}
)

// Retention policy defaults. They are applied in ModifyPlan rather than with
// schema defaults, which the framework does not support inside set elements.
const (
	defaultReplication        = 1
	defaultShardGroupDuration = "1h0m0s"
)

func newDatabaseResource() resource.Resource {
	return &databaseResource{}
}

type databaseResource struct {
	conn *client.Client
}

type databaseModel struct {
	ID                types.String           `tfsdk:"id"`
	Name              types.String           `tfsdk:"name"`
	RetentionPolicies []retentionPolicyModel `tfsdk:"retention_policies"`
}

type retentionPolicyModel struct {
	Name               types.String `tfsdk:"name"`
	Duration           types.String `tfsdk:"duration"`
	Replication        types.Int64  `tfsdk:"replication"`
	ShardGroupDuration types.String `tfsdk:"shardgroupduration"`
	Default            types.Bool   `tfsdk:"default"`
}

func (r *databaseResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_database"
}

func (r *databaseResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages an InfluxDB database and its retention policies.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "The database name.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:      true,
				Description:   "Name of the database.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
		},
		Blocks: map[string]schema.Block{
			"retention_policies": schema.SetNestedBlock{
				Description: "Retention policies of the database. The `autogen` policy InfluxDB creates with every database is only managed when it is listed here.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							Required:    true,
							Description: "Name of the retention policy.",
						},
						"duration": schema.StringAttribute{
							Required:    true,
							Description: "How long data is kept, e.g. `24h0m0s`. `0s` keeps data forever.",
						},
						"replication": schema.Int64Attribute{
							Optional:    true,
							Computed:    true,
							Description: "Replication factor. Defaults to `1`.",
						},
						"shardgroupduration": schema.StringAttribute{
							Optional:    true,
							Computed:    true,
							Description: "Shard group duration. Defaults to `1h0m0s`.",
						},
						"default": schema.BoolAttribute{
							Optional:    true,
							Computed:    true,
							Description: "Whether this is the default retention policy of the database. Defaults to `false`.",
						},
					},
				},
			},
		},
	}
}

// ModifyPlan plans the retention policies from configuration with the
// defaults filled in, so set elements are fully known.
func (r *databaseResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}

	var rps []retentionPolicyModel
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("retention_policies"), &rps)...)
	if resp.Diagnostics.HasError() {
		return
	}

	for i := range rps {
		if rps[i].Replication.IsNull() {
			rps[i].Replication = types.Int64Value(defaultReplication)
		}
		if rps[i].ShardGroupDuration.IsNull() {
			rps[i].ShardGroupDuration = types.StringValue(defaultShardGroupDuration)
		}
		if rps[i].Default.IsNull() {
			rps[i].Default = types.BoolValue(false)
		}
	}
	if rps == nil {
		rps = []retentionPolicyModel{}
	}

	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("retention_policies"), rps)...)
}

func (r *databaseResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	conn, err := clientFromProviderData(req.ProviderData)
	if err != nil {
		resp.Diagnostics.AddError("Unexpected provider data", err.Error())
		return
	}
	r.conn = conn
}

func (r *databaseResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan databaseModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := plan.Name.ValueString()
	if err := exec(r.conn, fmt.Sprintf("CREATE DATABASE %q", name)); err != nil {
		resp.Diagnostics.AddError("Unable to create database", err.Error())
		return
	}

	for _, rp := range plan.RetentionPolicies {
		if err := upsertRetentionPolicy(r.conn, rp, name); err != nil {
			resp.Diagnostics.AddError("Unable to create retention policy", err.Error())
			return
		}
	}

	plan.ID = types.StringValue(name)
	r.readInto(ctx, &plan, plan.RetentionPolicies, resp.State.Set, &resp.Diagnostics)
}

func (r *databaseResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state databaseModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	exists, err := databaseExists(r.conn, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to read databases", err.Error())
		return
	}
	if !exists {
		resp.State.RemoveResource(ctx)
		return
	}

	r.readInto(ctx, &state, state.RetentionPolicies, resp.State.Set, &resp.Diagnostics)
}

func (r *databaseResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state databaseModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := plan.Name.ValueString()

	oldRPs := map[string]bool{}
	for _, rp := range state.RetentionPolicies {
		oldRPs[rp.Name.ValueString()] = true
	}
	newRPs := map[string]bool{}
	for _, rp := range plan.RetentionPolicies {
		newRPs[rp.Name.ValueString()] = true
	}

	// Policies no longer configured are dropped.
	for _, rp := range state.RetentionPolicies {
		if !newRPs[rp.Name.ValueString()] {
			if err := exec(r.conn, fmt.Sprintf("DROP RETENTION POLICY %q ON %q", rp.Name.ValueString(), name)); err != nil {
				resp.Diagnostics.AddError("Unable to drop retention policy", err.Error())
				return
			}
		}
	}

	// New policies are created (or altered if they already exist, e.g.
	// autogen); existing ones are altered.
	for _, rp := range plan.RetentionPolicies {
		var err error
		if oldRPs[rp.Name.ValueString()] {
			err = retentionPolicyStatement(r.conn, "ALTER", rp, name)
		} else {
			err = upsertRetentionPolicy(r.conn, rp, name)
		}
		if err != nil {
			resp.Diagnostics.AddError("Unable to update retention policy", err.Error())
			return
		}
	}

	plan.ID = types.StringValue(name)
	r.readInto(ctx, &plan, plan.RetentionPolicies, resp.State.Set, &resp.Diagnostics)
}

func (r *databaseResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state databaseModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := exec(r.conn, fmt.Sprintf("DROP DATABASE %q", state.ID.ValueString())); err != nil {
		resp.Diagnostics.AddError("Unable to drop database", err.Error())
	}
}

func (r *databaseResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("retention_policies"), []retentionPolicyModel{})...)
}

// readInto reads the retention policies of the database in m from the server
// and stores m with set. prior holds the configured or previously stored
// policies; their duration strings are kept when the server reports the same
// duration in another form.
func (r *databaseResource) readInto(ctx context.Context, m *databaseModel, prior []retentionPolicyModel, set func(context.Context, any) diag.Diagnostics, diags *diag.Diagnostics) {
	rps, err := readRetentionPolicies(r.conn, m.ID.ValueString(), prior)
	if err != nil {
		diags.AddError("Unable to read retention policies", err.Error())
		return
	}

	m.Name = m.ID
	m.RetentionPolicies = rps
	diags.Append(set(ctx, m)...)
}

func databaseExists(conn *client.Client, name string) (bool, error) {
	// InfluxDB has no command to check for a single database, so the list of
	// all databases is searched.
	resp, err := queryResponse(conn, "SHOW DATABASES")
	if err != nil {
		return false, err
	}

	for _, row := range firstSeriesValues(resp) {
		if row[0] == name {
			return true, nil
		}
	}
	return false, nil
}

func retentionPolicyStatement(conn *client.Client, verb string, rp retentionPolicyModel, database string) error {
	query := fmt.Sprintf("%s RETENTION POLICY %q ON %q DURATION %s REPLICATION %d", verb, rp.Name.ValueString(), database, rp.Duration.ValueString(), rp.Replication.ValueInt64())
	if s := rp.ShardGroupDuration.ValueString(); s != "" {
		query += " SHARD DURATION " + s
	}
	if rp.Default.ValueBool() {
		query += " DEFAULT"
	}
	return exec(conn, query)
}

// upsertRetentionPolicy creates the retention policy, or alters it if it already exists, e.g. the
// "autogen" policy InfluxDB creates with every database.
func upsertRetentionPolicy(conn *client.Client, rp retentionPolicyModel, database string) error {
	resp, err := queryResponse(conn, fmt.Sprintf("SHOW RETENTION POLICIES ON %q", database))
	if err != nil {
		return err
	}

	verb := "CREATE"
	for _, series := range resp.Results[0].Series {
		for _, values := range series.Values {
			if len(values) > 0 && values[0] == rp.Name.ValueString() {
				verb = "ALTER"
			}
		}
	}

	return retentionPolicyStatement(conn, verb, rp, database)
}

func readRetentionPolicies(conn *client.Client, database string, prior []retentionPolicyModel) ([]retentionPolicyModel, error) {
	resp, err := queryResponse(conn, fmt.Sprintf("SHOW RETENTION POLICIES ON %q", database))
	if err != nil {
		return nil, err
	}

	// The default "autogen" policy InfluxDB creates with every database is
	// only tracked once it is managed, i.e. configured or in state.
	priorByName := map[string]retentionPolicyModel{}
	for _, rp := range prior {
		priorByName[rp.Name.ValueString()] = rp
	}

	rps := []retentionPolicyModel{}

	for _, series := range resp.Results[0].Series {
		// Look columns up by name: InfluxDB 1.12 added futureWriteLimit and
		// pastWriteLimit before "default", so positions differ between versions.
		col := map[string]int{}
		for _, name := range []string{"name", "duration", "shardGroupDuration", "replicaN", "default"} {
			i := slices.Index(series.Columns, name)
			if i < 0 {
				return nil, fmt.Errorf("SHOW RETENTION POLICIES: missing column %q (got %v)", name, series.Columns)
			}
			col[name] = i
		}

		for _, row := range series.Values {
			replication, err := row[col["replicaN"]].(json.Number).Int64()
			if err != nil {
				return nil, err
			}

			name, _ := row[col["name"]].(string)
			duration, _ := row[col["duration"]].(string)
			shard, _ := row[col["shardGroupDuration"]].(string)
			isDefault, _ := row[col["default"]].(bool)

			p, managed := priorByName[name]
			if name == "autogen" && !managed && duration == "0s" && shard == "168h0m0s" && replication == 1 && isDefault {
				continue
			}

			if managed {
				duration = keepEquivalentDuration(p.Duration, duration)
				shard = keepEquivalentDuration(p.ShardGroupDuration, shard)
			}

			rps = append(rps, retentionPolicyModel{
				Name:               types.StringValue(name),
				Duration:           types.StringValue(duration),
				Replication:        types.Int64Value(replication),
				ShardGroupDuration: types.StringValue(shard),
				Default:            types.BoolValue(isDefault),
			})
		}
	}

	return rps, nil
}
