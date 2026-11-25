package influxdb

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/influxdata/influxdb/client"
)

var (
	_ resource.Resource              = &continuousQueryResource{}
	_ resource.ResourceWithConfigure = &continuousQueryResource{}
)

func newContinuousQueryResource() resource.Resource {
	return &continuousQueryResource{}
}

type continuousQueryResource struct {
	conn *client.Client
}

type continuousQueryModel struct {
	ID       types.String `tfsdk:"id"`
	Name     types.String `tfsdk:"name"`
	Database types.String `tfsdk:"database"`
	Query    types.String `tfsdk:"query"`
	Resample types.String `tfsdk:"resample"`
}

func (r *continuousQueryResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_continuous_query"
}

func (r *continuousQueryResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}

	resp.Schema = schema.Schema{
		Description: "Manages an InfluxDB continuous query. Any change re-creates the query.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "`<name>:<database>`.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:      true,
				Description:   "Name of the continuous query.",
				PlanModifiers: replace,
			},
			"database": schema.StringAttribute{
				Required:      true,
				Description:   "Database the continuous query runs on.",
				PlanModifiers: replace,
			},
			"query": schema.StringAttribute{
				Required:      true,
				Description:   "The `SELECT` statement of the continuous query.",
				PlanModifiers: replace,
			},
			"resample": schema.StringAttribute{
				Optional:      true,
				Computed:      true,
				Default:       stringdefault.StaticString(""),
				Description:   "Optional `RESAMPLE` clause, e.g. `EVERY 30m FOR 90m`.",
				PlanModifiers: replace,
			},
		},
	}
}

func (r *continuousQueryResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	conn, err := clientFromProviderData(req.ProviderData)
	if err != nil {
		resp.Diagnostics.AddError("Unexpected provider data", err.Error())
		return
	}
	r.conn = conn
}

func (r *continuousQueryResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan continuousQueryModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := plan.Name.ValueString()
	database := plan.Database.ValueString()

	resample := ""
	if s := plan.Resample.ValueString(); s != "" {
		resample = " RESAMPLE " + s
	}

	if err := exec(r.conn, fmt.Sprintf("CREATE CONTINUOUS QUERY %q ON %q%s BEGIN %s END", name, database, resample, plan.Query.ValueString())); err != nil {
		resp.Diagnostics.AddError("Unable to create continuous query", err.Error())
		return
	}

	exists, err := continuousQueryExists(r.conn, name, database)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read continuous queries", err.Error())
		return
	}
	if !exists {
		resp.Diagnostics.AddError("Unable to create continuous query", fmt.Sprintf("Unable to create continuous query '%s', check your sql query.", name))
		return
	}

	plan.ID = types.StringValue(fmt.Sprintf("%s:%s", name, database))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *continuousQueryResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state continuousQueryModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name, database, err := continuousQueryID(state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid continuous query ID", err.Error())
		return
	}

	exists, err := continuousQueryExists(r.conn, name, database)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read continuous queries", err.Error())
		return
	}
	if !exists {
		resp.State.RemoveResource(ctx)
		return
	}

	state.Name = types.StringValue(name)
	state.Database = types.StringValue(database)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update is never called: every attribute requires replacement.
func (r *continuousQueryResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Continuous queries cannot be updated", "Every change re-creates the continuous query.")
}

func (r *continuousQueryResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state continuousQueryModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := exec(r.conn, fmt.Sprintf("DROP CONTINUOUS QUERY %q ON %q", state.Name.ValueString(), state.Database.ValueString())); err != nil {
		resp.Diagnostics.AddError("Unable to drop continuous query", err.Error())
	}
}

func continuousQueryExists(conn *client.Client, name, database string) (bool, error) {
	// InfluxDB has no command to check for a single continuous query, so the
	// list of all continuous queries is searched.
	resp, err := queryResponse(conn, "SHOW CONTINUOUS QUERIES")
	if err != nil {
		return false, err
	}

	for _, series := range resp.Results[0].Series {
		if series.Name != database {
			continue
		}
		for _, row := range series.Values {
			if row[0] == name {
				return true, nil
			}
		}
	}
	return false, nil
}

func continuousQueryID(id string) (string, string, error) {
	parts := strings.SplitN(id, ":", 2)

	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("unexpected format of ID (%s), expected NAME:DB-ID", id)
	}

	return parts[0], parts[1], nil
}
