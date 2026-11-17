package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/adaptor"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/google/uuid"
	"github.com/hertz-contrib/sse"

	"github.com/a2aproject/a2a-go/v2/a2asrv"

	"github.com/kelvins-io/eino-bot/internal/agent"
	"github.com/kelvins-io/eino-bot/internal/asr"
)

type Server struct {
	rt  *agent.Runtime
	asr *asr.Client
	h   *server.Hertz
}

func New(rt *agent.Runtime, asrClient *asr.Client) *Server {
	h := server.Default(server.WithHostPorts(rt.Cfg.Addr()))
	s := &Server{rt: rt, asr: asrClient, h: h}
	s.routes()
	return s
}

func (s *Server) Run() {
	log.Printf("eino-bot listening on http://%s", s.rt.Cfg.Addr())
	log.Printf("bot_id=%s public=%s a2a_card=%s%s",
		s.rt.Cfg.BotID, s.rt.Cfg.BotPublicURL, s.rt.Cfg.BotPublicURL, a2asrv.WellKnownAgentCardPath)
	s.h.Spin()
}

func (s *Server) routes() {
	s.h.GET("/healthz", func(ctx context.Context, c *app.RequestContext) {
		c.JSON(consts.StatusOK, map[string]any{"ok": true, "bot_id": s.rt.Cfg.BotID})
	})
	s.h.GET("/v1/info", s.handleInfo)
	s.h.GET("/v1/chat", s.handleChatSSE)
	s.h.POST("/v1/chat", s.handleChatJSON)
	s.h.POST("/v1/voice", s.handleVoice)
	s.h.GET("/v1/profile", s.handleProfile)
	s.h.POST("/v1/recruit/market/callback", s.handleMarketRecruitCallback)
	s.h.GET("/v1/recruit/market/tasks", s.handleMarketRecruitTasks)

	// Public A2A protocol (Agent Card + JSON-RPC).
	s.h.GET(a2asrv.WellKnownAgentCardPath, adaptor.HertzHandler(s.rt.A2A.CardHandler()))
	s.h.Any(s.rt.A2A.A2APath, adaptor.HertzHandler(s.rt.A2A.JSONRPCHandler()))

	// Optional embedded A2A registry catalog.
	if s.rt.A2A.HostRegistry() {
		reg := s.rt.A2A.RegistryHandler()
		for _, p := range []string{
			"/registry/register",
			"/registry/unregister",
			"/registry/heartbeat",
			"/registry/agents",
			"/agents",
		} {
			path := p
			s.h.Any(path, adaptor.HertzHandler(reg))
		}
	}

	// Compatibility aliases for older clients.
	s.h.GET("/v1/a2a/discover", s.handleA2ADiscover)
	s.h.Any("/v1/a2a/message", func(ctx context.Context, c *app.RequestContext) {
		c.JSON(consts.StatusGone, map[string]any{
			"error":            "legacy HMAC A2A endpoint removed",
			"use_agent_card":   s.rt.Cfg.BotPublicURL + a2asrv.WellKnownAgentCardPath,
			"use_jsonrpc_path": s.rt.Cfg.BotPublicURL + s.rt.A2A.A2APath,
			"protocol":         "a2a",
		})
	})
}

func (s *Server) handleInfo(ctx context.Context, c *app.RequestContext) {
	c.JSON(consts.StatusOK, map[string]any{
		"bot_id":      s.rt.Cfg.BotID,
		"bot_name":    s.rt.Cfg.BotName,
		"public_url":  s.rt.Cfg.BotPublicURL,
		"owner_id":    s.rt.OwnerID,
		"docs_root":   s.rt.Cfg.DocsRoot,
		"resume_root": s.rt.Cfg.ResumeRoot,
		"model_type":  s.rt.Cfg.ModelType,
		"model":       s.rt.Cfg.OpenAIModel,
		"model_base":  s.rt.Cfg.OpenAIBaseURL,
		"a2a":         s.rt.A2A.DiscoverSummary(),
		"endpoints": []string{
			"GET /v1/chat?query=...&session_id=...",
			"POST /v1/chat",
			"POST /v1/voice",
			"GET /v1/profile?owner_id=...",
			"GET " + a2asrv.WellKnownAgentCardPath,
			"POST " + s.rt.A2A.A2APath + " (A2A JSON-RPC)",
			"POST /v1/recruit/market/callback",
			"GET /v1/recruit/market/tasks?job_id=...",
		},
	})
}

type marketCallbackBody struct {
	TaskID     string           `json:"task_id"`
	JobID      string           `json:"job_id"`
	Status     string           `json:"status"`
	Token      string           `json:"token"`
	Candidates []map[string]any `json:"candidates"`
	Error      string           `json:"error"`
}

func (s *Server) handleMarketRecruitCallback(ctx context.Context, c *app.RequestContext) {
	raw, err := c.Body()
	if err != nil {
		c.JSON(consts.StatusBadRequest, map[string]any{"ok": false, "error": "empty body"})
		return
	}
	var body marketCallbackBody
	if err := json.Unmarshal(raw, &body); err != nil {
		c.JSON(consts.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	token := body.Token
	if token == "" {
		token = string(c.GetHeader("X-Callback-Token"))
	}
	var taskID string
	if token != "" {
		t, err := s.rt.Memory.GetMarketRecruitTaskByToken(ctx, token)
		if err != nil {
			c.JSON(consts.StatusUnauthorized, map[string]any{"ok": false, "error": "invalid callback token"})
			return
		}
		taskID = t.ID
	} else if body.TaskID != "" {
		taskID = body.TaskID
	} else {
		c.JSON(consts.StatusBadRequest, map[string]any{"ok": false, "error": "task_id or token required"})
		return
	}
	updated, err := s.rt.Memory.CompleteMarketRecruitTask(ctx, taskID, body.Candidates, body.Error)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(consts.StatusOK, map[string]any{"ok": true, "task": updated})
}

func (s *Server) handleMarketRecruitTasks(ctx context.Context, c *app.RequestContext) {
	jobID := string(c.Query("job_id"))
	taskID := string(c.Query("task_id"))
	if taskID != "" {
		t, err := s.rt.Memory.GetMarketRecruitTask(ctx, taskID)
		if err != nil {
			c.JSON(consts.StatusNotFound, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"ok": true, "task": t})
		return
	}
	list, err := s.rt.Memory.ListMarketRecruitTasks(ctx, jobID)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(consts.StatusOK, map[string]any{"ok": true, "tasks": list})
}

func (s *Server) handleProfile(ctx context.Context, c *app.RequestContext) {
	owner := string(c.Query("owner_id"))
	if owner == "" {
		owner = s.rt.OwnerID
	}
	p, err := s.rt.Memory.GetProfile(ctx, owner)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	c.JSON(consts.StatusOK, p)
}

type chatReq struct {
	Query     string `json:"query"`
	SessionID string `json:"session_id"`
	OwnerID   string `json:"owner_id"`
}

func (s *Server) handleChatJSON(ctx context.Context, c *app.RequestContext) {
	var req chatReq
	if err := c.BindAndValidate(&req); err != nil {
		c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if strings.TrimSpace(req.Query) == "" {
		c.JSON(consts.StatusBadRequest, map[string]string{"error": "query required"})
		return
	}
	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = uuid.NewString()
	}
	text, err := s.runTurn(ctx, sessionID, req.Query)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	c.JSON(consts.StatusOK, map[string]any{
		"session_id": sessionID,
		"reply":      text,
	})
}

func (s *Server) handleChatSSE(ctx context.Context, c *app.RequestContext) {
	query := string(c.Query("query"))
	if query == "" {
		c.JSON(consts.StatusBadRequest, map[string]string{"error": "query parameter is required"})
		return
	}
	sessionID := string(c.Query("session_id"))
	if sessionID == "" {
		sessionID = uuid.NewString()
	}
	_ = s.rt.Memory.AppendChat(ctx, sessionID, "user", query)

	stream := sse.NewStream(c)
	defer func() { _ = c.Flush() }()

	_ = publish(stream, map[string]any{"type": "session", "session_id": sessionID})

	iter := s.rt.Runner.Query(ctx, query, adk.WithCheckPointID(sessionID))
	var final strings.Builder
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			_ = publish(stream, map[string]any{"type": "error", "error": event.Err.Error()})
			break
		}
		chunk, err := consumeEvent(event)
		if err != nil {
			_ = publish(stream, map[string]any{"type": "error", "error": err.Error()})
			break
		}
		if chunk != "" {
			final.WriteString(chunk)
			_ = publish(stream, map[string]any{"type": "stream_chunk", "content": chunk, "agent": event.AgentName})
		}
		if event.Action != nil && event.Action.Exit {
			_ = publish(stream, map[string]any{"type": "action", "action_type": "exit"})
		}
	}
	reply := strings.TrimSpace(final.String())
	if reply != "" {
		_ = s.rt.Memory.AppendChat(ctx, sessionID, "assistant", reply)
	}
	_ = publish(stream, map[string]any{"type": "done", "session_id": sessionID, "reply": reply})
}

func (s *Server) runTurn(ctx context.Context, sessionID, query string) (string, error) {
	_ = s.rt.Memory.AppendChat(ctx, sessionID, "user", query)
	iter := s.rt.Runner.Query(ctx, query, adk.WithCheckPointID(sessionID))
	var final strings.Builder
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			return "", event.Err
		}
		chunk, err := consumeEvent(event)
		if err != nil {
			return "", err
		}
		final.WriteString(chunk)
	}
	reply := strings.TrimSpace(final.String())
	if reply != "" {
		_ = s.rt.Memory.AppendChat(ctx, sessionID, "assistant", reply)
	}
	return reply, nil
}

func consumeEvent(event *adk.AgentEvent) (string, error) {
	if event.Output == nil || event.Output.MessageOutput == nil {
		return "", nil
	}
	msgOut := event.Output.MessageOutput
	if msgOut.Message != nil {
		if msgOut.Message.Role == schema.Assistant || msgOut.Message.Role == "" {
			return msgOut.Message.Content, nil
		}
		return "", nil
	}
	if msgOut.MessageStream == nil {
		return "", nil
	}
	var b strings.Builder
	for {
		chunk, err := msgOut.MessageStream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return b.String(), err
		}
		if chunk != nil && chunk.Content != "" && (chunk.Role == schema.Assistant || chunk.Role == "") {
			b.WriteString(chunk.Content)
		}
	}
	return b.String(), nil
}

func (s *Server) handleVoice(ctx context.Context, c *app.RequestContext) {
	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(consts.StatusBadRequest, map[string]string{"error": "multipart file field 'file' required"})
		return
	}
	f, err := fileHeader.Open()
	if err != nil {
		c.JSON(consts.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	defer f.Close()
	raw, err := io.ReadAll(f)
	if err != nil {
		c.JSON(consts.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	text, err := s.asr.Transcribe(ctx, fileHeader.Filename, raw)
	if err != nil {
		c.JSON(consts.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	sessionID := string(c.FormValue("session_id"))
	if sessionID == "" {
		sessionID = uuid.NewString()
	}
	runAgent := string(c.FormValue("run_agent"))
	if runAgent == "false" || runAgent == "0" {
		c.JSON(consts.StatusOK, map[string]any{"session_id": sessionID, "transcript": text})
		return
	}
	reply, err := s.runTurn(ctx, sessionID, text)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, map[string]any{
			"session_id": sessionID, "transcript": text, "error": err.Error(),
		})
		return
	}
	c.JSON(consts.StatusOK, map[string]any{
		"session_id": sessionID, "transcript": text, "reply": reply,
	})
}

func (s *Server) handleA2ADiscover(ctx context.Context, c *app.RequestContext) {
	c.JSON(consts.StatusOK, s.rt.A2A.DiscoverSummary())
}

func publish(stream *sse.Stream, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return stream.Publish(&sse.Event{Data: data})
}
