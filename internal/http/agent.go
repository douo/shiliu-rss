package httpapi

import (
	"context"
	"encoding/json"
	"shiliu-rss/api"
	"shiliu-rss/internal/app"
	"strings"
)

func (s *Server) agentWake() {
	if s.Agent != nil {
		s.Agent.Wake()
	}
}
func (s *Server) AgentGetSettings(ctx context.Context, r api.AgentGetSettingsRequestObject) (api.AgentGetSettingsResponseObject, error) {
	v, e := s.App.AgentSettings(ctx)
	if e != nil {
		return nil, e
	}
	return convert[api.AgentGetSettings200JSONResponse](v)
}
func (s *Server) AgentSaveSettings(ctx context.Context, r api.AgentSaveSettingsRequestObject) (api.AgentSaveSettingsResponseObject, error) {
	if r.Body == nil {
		return nil, app.ErrInvalid
	}
	v, e := convert[app.AgentSettings](r.Body.Settings)
	if e != nil {
		return nil, e
	}
	v, e = s.App.SaveAgentSettings(ctx, v, r.Body.ApiKey)
	if e != nil {
		return nil, e
	}
	s.agentWake()
	return convert[api.AgentSaveSettings200JSONResponse](v)
}
func (s *Server) AgentGetUsage(ctx context.Context, r api.AgentGetUsageRequestObject) (api.AgentGetUsageResponseObject, error) {
	v, e := s.App.AgentSettings(ctx)
	if e != nil {
		return nil, e
	}
	out, e := s.App.AgentUsage(ctx, v)
	if e != nil {
		return nil, e
	}
	return convert[api.AgentGetUsage200JSONResponse](out)
}
func (s *Server) agentQueue(ctx context.Context, key, kind string, input any) (app.AgentRun, error) {
	ctx, e := idempotent(ctx, key, "agent:"+kind, input)
	if e != nil {
		return app.AgentRun{}, e
	}
	raw, e := json.Marshal(input)
	if e != nil {
		return app.AgentRun{}, e
	}
	v, e := s.App.QueueAgentRun(ctx, kind, string(raw), "")
	if e == nil {
		s.agentWake()
	}
	return v, e
}
func (s *Server) AgentTestConnection(ctx context.Context, r api.AgentTestConnectionRequestObject) (api.AgentTestConnectionResponseObject, error) {
	v, e := s.agentQueue(ctx, r.Params.IdempotencyKey, "query", map[string]bool{"test": true})
	if e != nil {
		return nil, e
	}
	return convert[api.AgentTestConnection202JSONResponse](v)
}
func (s *Server) AgentCreateReport(ctx context.Context, r api.AgentCreateReportRequestObject) (api.AgentCreateReportResponseObject, error) {
	v, e := s.agentQueue(ctx, r.Params.IdempotencyKey, "report", map[string]string{})
	if e != nil {
		return nil, e
	}
	return convert[api.AgentCreateReport202JSONResponse](v)
}
func (s *Server) AgentQuery(ctx context.Context, r api.AgentQueryRequestObject) (api.AgentQueryResponseObject, error) {
	if r.Body == nil || strings.TrimSpace(r.Body.Question) == "" || len([]rune(r.Body.Question)) > 10000 {
		return nil, app.ErrInvalid
	}
	if val(r.Body.ReportId) != "" {
		v, e := s.App.GetAgentRun(ctx, val(r.Body.ReportId))
		if e != nil {
			return nil, e
		}
		if v.Kind != "report" {
			return nil, app.ErrInvalid
		}
	}
	v, e := s.agentQueue(ctx, r.Params.IdempotencyKey, "query", r.Body)
	if e != nil {
		return nil, e
	}
	return convert[api.AgentQuery202JSONResponse](v)
}
func (s *Server) AgentListRuns(ctx context.Context, r api.AgentListRunsRequestObject) (api.AgentListRunsResponseObject, error) {
	kind := val(r.Params.Kind)
	if kind != "" && kind != "report" && kind != "query" && kind != "chat" {
		return nil, app.ErrInvalid
	}
	v, e := s.App.ListAgentRuns(ctx, kind, val(r.Params.Before), val(r.Params.Limit))
	if e != nil {
		return nil, e
	}
	return convert[api.AgentListRuns200JSONResponse](v)
}
func (s *Server) AgentGetRun(ctx context.Context, r api.AgentGetRunRequestObject) (api.AgentGetRunResponseObject, error) {
	v, e := s.App.GetAgentRun(ctx, r.Id)
	if e != nil {
		return nil, e
	}
	return convert[api.AgentGetRun200JSONResponse](v)
}
func (s *Server) AgentControlRun(ctx context.Context, r api.AgentControlRunRequestObject) (api.AgentControlRunResponseObject, error) {
	if r.Body == nil {
		return nil, app.ErrInvalid
	}
	v, e := s.App.ControlAgentRun(ctx, r.Id, string(r.Body.Action))
	if e != nil {
		return nil, e
	}
	s.agentWake()
	return convert[api.AgentControlRun200JSONResponse](v)
}
func (s *Server) AgentListMembers(ctx context.Context, r api.AgentListMembersRequestObject) (api.AgentListMembersResponseObject, error) {
	run, e := s.App.GetAgentRun(ctx, r.Id)
	if e != nil {
		return nil, e
	}
	if run.Kind != "report" {
		return nil, app.ErrInvalid
	}
	v, e := s.App.AgentMembers(ctx, r.Id, val(r.Params.After), val(r.Params.FeedId), val(r.Params.Limit), false)
	if e != nil {
		return nil, e
	}
	return convert[api.AgentListMembers200JSONResponse](v)
}
func (s *Server) AgentMarkReportRead(ctx context.Context, r api.AgentMarkReportReadRequestObject) (api.AgentMarkReportReadResponseObject, error) {
	var input any = r.Id
	if r.Params.GroupId != nil {
		if *r.Params.GroupId == "" {
			return nil, app.ErrInvalid
		}
		input = map[string]string{"report_id": r.Id, "group_id": *r.Params.GroupId}
	}
	ctx, e := idempotent(ctx, r.Params.IdempotencyKey, "agent:report-read:"+r.Id, input)
	if e != nil {
		return nil, e
	}
	var v app.AgentReadResult
	if r.Params.GroupId != nil {
		v, e = s.App.MarkAgentReportGroupRead(ctx, r.Id, *r.Params.GroupId)
	} else {
		v, e = s.App.MarkAgentReportRead(ctx, r.Id)
	}
	if e != nil {
		return nil, e
	}
	return convert[api.AgentMarkReportRead200JSONResponse](v)
}
func (s *Server) AgentSubmitReport(ctx context.Context, r api.AgentSubmitReportRequestObject) (api.AgentSubmitReportResponseObject, error) {
	if r.Body == nil {
		return nil, app.ErrInvalid
	}
	result, e := convert[app.AgentResult](r.Body)
	if e != nil {
		return nil, e
	}
	v, e := s.App.SubmitAgentReport(ctx, r.Id, result)
	if e != nil {
		return nil, e
	}
	return convert[api.AgentSubmitReport200JSONResponse](v)
}
func (s *Server) AgentListConversations(ctx context.Context, r api.AgentListConversationsRequestObject) (api.AgentListConversationsResponseObject, error) {
	v, e := s.App.AgentConversations(ctx, val(r.Params.Before))
	if e != nil {
		return nil, e
	}
	return convert[api.AgentListConversations200JSONResponse](v)
}
func (s *Server) AgentCreateConversation(ctx context.Context, r api.AgentCreateConversationRequestObject) (api.AgentCreateConversationResponseObject, error) {
	if r.Body == nil {
		return nil, app.ErrInvalid
	}
	ctx, e := idempotent(ctx, r.Params.IdempotencyKey, "agent:conversation", r.Body)
	if e != nil {
		return nil, e
	}
	v, e := s.App.CreateAgentConversation(ctx, val(r.Body.Title), val(r.Body.ReportId))
	if e != nil {
		return nil, e
	}
	return convert[api.AgentCreateConversation201JSONResponse](v)
}
func (s *Server) AgentListMessages(ctx context.Context, r api.AgentListMessagesRequestObject) (api.AgentListMessagesResponseObject, error) {
	v, e := s.App.AgentMessages(ctx, r.Id, val(r.Params.Before))
	if e != nil {
		return nil, e
	}
	return convert[api.AgentListMessages200JSONResponse](v)
}
func (s *Server) AgentSendMessage(ctx context.Context, r api.AgentSendMessageRequestObject) (api.AgentSendMessageResponseObject, error) {
	if r.Body == nil {
		return nil, app.ErrInvalid
	}
	ctx, e := idempotent(ctx, r.Params.IdempotencyKey, "agent:message:"+r.Id, r.Body)
	if e != nil {
		return nil, e
	}
	v, e := s.App.SendAgentMessage(ctx, r.Id, r.Body.Text)
	if e != nil {
		return nil, e
	}
	s.agentWake()
	return convert[api.AgentSendMessage202JSONResponse](v)
}
