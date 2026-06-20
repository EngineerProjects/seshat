package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/EngineerProjects/nexus-engine/internal/providers"
	engineconfig "github.com/EngineerProjects/nexus-engine/pkg/config"
	"github.com/EngineerProjects/nexus-engine/pkg/sdk"
	slackgo "github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

const defaultModel = "mistral:mistral-small-latest"

// streamInterval is how often we push incremental text updates to Slack.
// Slack rate-limits UpdateMessage; 1.5s is safe.
const streamInterval = 1500 * time.Millisecond

// slackMaxLen is the Slack text field character limit for chat.update / chat.postMessage.
const slackMaxLen = 2900

type bot struct {
	nexus *sdk.Client
	api   *slackgo.Client

	mu       sync.Mutex
	sessions map[string]sdk.SessionID // channelID → nexus sessionID

	// streamMu serialises SetResponseChunkFn so concurrent requests don't
	// overwrite each other's streaming callback.
	streamMu sync.Mutex
}

func main() {
	botToken := mustEnv("NEXUS_SLACK_BOT_TOKEN")
	appToken := mustEnv("NEXUS_SLACK_APP_TOKEN")

	cfg, err := engineconfig.Load()
	if err != nil {
		log.Fatalf("[nexus-bot] config: %v", err)
	}
	if strings.TrimSpace(cfg.Model) == "" {
		cfg.Model = defaultModel
	}

	model := resolveModel(cfg)
	apiKey := engineconfig.ResolveAPIKey(cfg, model.Provider)

	providerCfg := providers.GetProviderConfig(model.Provider)
	if providerCfg == nil {
		providerCfg = &providers.Config{Provider: model.Provider}
	}
	providerCfg.APIKey = apiKey
	if cfg.ProviderBaseURL != "" {
		providerCfg.BaseURL = cfg.ProviderBaseURL
	}

	slackPrompt := `You are Nexus, an AI assistant integrated into Slack via the Nexus Engine runtime.

Key rules:
- This is a PERSISTENT conversation. Each channel has one continuous session — prior messages are in your context.
- NEVER repeat a web search you already performed in this conversation. Use existing search results from your context.
- Be concise. Slack messages have a 3000-character limit — prefer bullet points over long prose.
- When the user says "based on that" or "from this", they refer to your previous response in this conversation.
- You can use all Nexus tools: web search, file operations, memory, sub-agents, and connected MCP servers.`

	nexusClient, err := sdk.NewClient(&sdk.ClientConfig{
		APIKey:            apiKey,
		Model:             model,
		PermissionMode:    sdk.PermissionModeBypass,
		AutoCompact:       true,
		PersistSessions:   true,
		SessionSQLitePath: nexusDBPath(),
		WorkingDir:        workdir(),
		ProviderConfig:    providerCfg,
		PromptConfig: &sdk.PromptConfig{
			AppendSystemPrompt: &slackPrompt,
		},
	})
	if err != nil {
		log.Fatalf("[nexus-bot] nexus client: %v", err)
	}
	defer nexusClient.Close()

	api := slackgo.New(botToken, slackgo.OptionAppLevelToken(appToken))
	sm := socketmode.New(api)

	b := &bot{
		nexus:    nexusClient,
		api:      api,
		sessions: make(map[string]sdk.SessionID),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go handleSignals(cancel)
	go b.handleEvents(ctx, sm)

	log.Printf("[nexus-bot] ready — model: %s/%s", model.Provider, model.Model)
	if err := sm.RunContext(ctx); err != nil && err != context.Canceled {
		log.Fatalf("[nexus-bot] socket mode: %v", err)
	}
}

func (b *bot) handleEvents(ctx context.Context, sm *socketmode.Client) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-sm.Events:
			if !ok {
				return
			}
			switch evt.Type {
			case socketmode.EventTypeConnecting,
				socketmode.EventTypeConnectionError,
				socketmode.EventTypeConnected,
				socketmode.EventTypeHello,
				socketmode.EventTypeInvalidAuth,
				socketmode.EventTypeDisconnect:
				log.Printf("[nexus-bot] socket: %s", evt.Type)

			case socketmode.EventTypeEventsAPI:
				ev, ok := evt.Data.(slackevents.EventsAPIEvent)
				if !ok || evt.Request == nil {
					continue
				}
				if err := sm.Ack(*evt.Request); err != nil {
					log.Printf("[nexus-bot] ack error: %v", err)
				}
				b.dispatch(ctx, ev)

			default:
				if evt.Request != nil {
					if err := sm.Ack(*evt.Request); err != nil {
						log.Printf("[nexus-bot] ack error: %v", err)
					}
				}
			}
		}
	}
}

func (b *bot) dispatch(ctx context.Context, ev slackevents.EventsAPIEvent) {
	switch inner := ev.InnerEvent.Data.(type) {
	case *slackevents.AppMentionEvent:
		if inner.BotID != "" {
			return
		}
		// Reply in the existing thread when @Nexus is mentioned inside one,
		// otherwise start a new thread off the mention message.
		replyTS := inner.ThreadTimeStamp
		if replyTS == "" {
			replyTS = inner.TimeStamp
		}
		go b.onMessage(ctx, inner.Channel, replyTS, inner.Text)

	case *slackevents.MessageEvent:
		// DMs only — skip bot messages and subtypes (edits, joins, etc.)
		if inner.BotID != "" || inner.SubType != "" {
			return
		}
		if ev.InnerEvent.Type == "message" {
			go b.onMessage(ctx, inner.Channel, inner.TimeStamp, inner.Text)
		}
	}
}

func (b *bot) onMessage(ctx context.Context, channel, replyTS, text string) {
	query := stripMention(text)
	if query == "" {
		return
	}

	log.Printf("[nexus-bot] message channel=%s query=%q", channel, query)

	_, thinkTS, err := b.api.PostMessageContext(ctx, channel,
		slackgo.MsgOptionText(":hourglass_flowing_sand: _Nexus is thinking..._", false),
		slackgo.MsgOptionTS(replyTS),
		slackgo.MsgOptionDisableLinkUnfurl(),
	)
	if err != nil {
		log.Printf("[nexus-bot] post placeholder: %v", err)
		return
	}

	session, err := b.getOrCreateSession(ctx, channel)
	if err != nil {
		b.updateMsg(ctx, channel, thinkTS, fmt.Sprintf(":x: Could not start session: %v", err))
		return
	}

	// ── Streaming ──────────────────────────────────────────────────────────────
	// Accumulate text deltas and push to Slack every streamInterval.
	// streamMu ensures a single active callback at a time (concurrent channels).
	b.streamMu.Lock()
	var (
		accMu   sync.Mutex
		accText string
	)
	b.nexus.SetResponseChunkFn(func(chunk sdk.ResponseChunk) {
		if chunk.Delta != "" {
			accMu.Lock()
			accText += chunk.Delta
			accMu.Unlock()
		}
	})
	b.streamMu.Unlock()

	streamDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(streamInterval)
		defer ticker.Stop()
		var lastLen int
		for {
			select {
			case <-streamDone:
				return
			case <-ticker.C:
				accMu.Lock()
				t := accText
				accMu.Unlock()
				if len(t) > lastLen {
					lastLen = len(t)
					b.updateMsg(ctx, channel, thinkTS, slackTrunc(t, slackMaxLen-1)+"▌")
				}
			}
		}
	}()

	t0 := time.Now()
	resp, err := session.SubmitMessage(ctx, query)

	close(streamDone)
	b.streamMu.Lock()
	b.nexus.SetResponseChunkFn(nil)
	b.streamMu.Unlock()

	if err != nil {
		b.updateMsg(ctx, channel, thinkTS, fmt.Sprintf(":x: Agent error: %v", err))
		return
	}

	answer := mdToMrkdwn(extractAnswer(resp))
	if answer == "" {
		answer = "_No response generated._"
	}

	elapsed := time.Since(t0).Round(time.Millisecond)
	footer := fmt.Sprintf("\n\n_— Nexus for Slack · %dms_", elapsed.Milliseconds())

	if tools := extractToolsUsed(resp); len(tools) > 0 {
		log.Printf("[nexus-bot] tools used: %s", strings.Join(tools, ", "))
		footer += fmt.Sprintf(" · 🔧 _%s_", strings.Join(tools, ", "))
	}

	// Split long responses into multiple thread messages.
	chunks := splitForSlack(answer, footer, slackMaxLen)
	b.updateMsg(ctx, channel, thinkTS, chunks[0])
	for _, extra := range chunks[1:] {
		if _, _, err := b.api.PostMessageContext(ctx, channel,
			slackgo.MsgOptionText(extra, false),
			slackgo.MsgOptionTS(replyTS),
			slackgo.MsgOptionDisableLinkUnfurl(),
		); err != nil {
			log.Printf("[nexus-bot] post continuation: %v", err)
		}
	}
}

func (b *bot) getOrCreateSession(ctx context.Context, channelID string) (*sdk.Session, error) {
	b.mu.Lock()
	sessionID, exists := b.sessions[channelID]
	b.mu.Unlock()

	if exists {
		s, err := b.nexus.LoadSession(ctx, sessionID)
		if err == nil {
			return s, nil
		}
		log.Printf("[nexus-bot] reload session %s failed (%v) — creating new", sessionID, err)
	}

	s, err := b.nexus.CreateSessionWithAdditional(ctx, map[string]any{
		"slack_channel": channelID,
		"source":        "nexus-slack-bot",
	})
	if err != nil {
		return nil, err
	}

	b.mu.Lock()
	b.sessions[channelID] = s.GetID()
	b.mu.Unlock()

	log.Printf("[nexus-bot] new session %s for channel %s", s.GetID(), channelID)
	return s, nil
}

func (b *bot) updateMsg(ctx context.Context, channel, ts, text string) {
	_, _, _, err := b.api.UpdateMessageContext(ctx, channel, ts,
		slackgo.MsgOptionText(text, false),
		slackgo.MsgOptionDisableLinkUnfurl(),
	)
	if err != nil {
		log.Printf("[nexus-bot] update message: %v", err)
	}
}

// extractAnswer pulls only the current-turn assistant text from the session response.
// resp.Messages contains the full conversation history; we skip everything up to
// and including the last user message so we never repeat previous assistant turns.
func extractAnswer(resp *sdk.SessionResponse) string {
	// Find the index of the last user message.
	lastUserIdx := -1
	for i, msg := range resp.Messages {
		if msg.Role == sdk.RoleUser {
			lastUserIdx = i
		}
	}

	var sb strings.Builder
	for i, msg := range resp.Messages {
		if i <= lastUserIdx {
			continue
		}
		if msg.Role != sdk.RoleAssistant {
			continue
		}
		for _, block := range msg.Content {
			if tc, ok := block.(sdk.TextContent); ok && tc.Text != "" {
				sb.WriteString(tc.Text)
			}
		}
	}
	return strings.TrimSpace(sb.String())
}

// extractToolsUsed returns deduplicated tool names called during the response.
func extractToolsUsed(resp *sdk.SessionResponse) []string {
	seen := map[string]bool{}
	var tools []string
	for _, msg := range resp.Messages {
		for _, block := range msg.Content {
			if tu, ok := block.(sdk.ToolUseContent); ok && !seen[tu.Name] {
				seen[tu.Name] = true
				tools = append(tools, tu.Name)
			}
		}
	}
	return tools
}

// mdToMrkdwn converts standard Markdown to Slack mrkdwn.
// Slack uses *bold*, _italic_, ~strike~, `code`, ```block```, <url|text>.
var (
	reMdBoldItalic = regexp.MustCompile(`\*\*\*(.+?)\*\*\*`)
	reMdBold       = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reMdStrike     = regexp.MustCompile(`~~(.+?)~~`)
	reMdLink       = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	reMdHeader     = regexp.MustCompile(`(?m)^#{1,6}\s+(.+)$`)
	reMdHRule      = regexp.MustCompile(`(?m)^---+\s*$`)
)

func mdToMrkdwn(s string) string {
	s = reMdBoldItalic.ReplaceAllString(s, "*_$1_*")
	s = reMdBold.ReplaceAllString(s, "*$1*")
	s = reMdStrike.ReplaceAllString(s, "~$1~")
	s = reMdLink.ReplaceAllString(s, "<$2|$1>")
	s = reMdHeader.ReplaceAllString(s, "*$1*")
	s = reMdHRule.ReplaceAllString(s, "")
	return s
}

// splitForSlack splits answer+footer into chunks that fit within maxLen.
// The footer is appended to the last chunk only.
func splitForSlack(answer, footer string, maxLen int) []string {
	if len(answer)+len(footer) <= maxLen {
		return []string{answer + footer}
	}
	var chunks []string
	remaining := answer
	for len(remaining) > 0 {
		limit := maxLen
		isLast := len(remaining) <= limit
		if isLast {
			if len(remaining)+len(footer) <= maxLen {
				chunks = append(chunks, remaining+footer)
			} else {
				chunks = append(chunks, remaining)
				chunks = append(chunks, footer)
			}
			break
		}
		// prefer cutting at a newline within the last 300 chars
		cut := limit
		for i := cut; i > limit-300 && i > 0; i-- {
			if remaining[i] == '\n' {
				cut = i + 1
				break
			}
		}
		chunks = append(chunks, remaining[:cut])
		remaining = strings.TrimSpace(remaining[cut:])
	}
	return chunks
}

// slackTrunc truncates s to max runes, cutting at a word boundary when possible.
func slackTrunc(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max - 1
	for cut > max-50 && cut > 0 && s[cut] != ' ' && s[cut] != '\n' {
		cut--
	}
	return s[:cut] + "…"
}

// stripMention removes <@UXXXXXXX> Slack mention syntax from text.
func stripMention(text string) string {
	s := text
	for strings.Contains(s, "<@") {
		start := strings.Index(s, "<@")
		end := strings.Index(s[start:], ">")
		if end == -1 {
			break
		}
		s = s[:start] + s[start+end+1:]
	}
	return strings.TrimSpace(s)
}

func resolveModel(cfg engineconfig.Config) sdk.ModelIdentifier {
	raw := strings.TrimSpace(cfg.Model)
	model := engineconfig.ParseModelIdentifier(raw)
	if engineconfig.HasExplicitProviderPrefix(raw) {
		return model
	}
	provider := engineconfig.DetectProviderFromModel(raw)
	if provider == "" {
		_, provider = engineconfig.EffectiveAPIKeyAndProvider(cfg)
	}
	if provider == "" {
		provider = model.Provider
	}
	model.Provider = provider
	return model
}

func nexusDBPath() string {
	if p := os.Getenv("NEXUS_SLACK_DB_PATH"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return home + "/.config/nexus-slack/sessions.db"
}

func workdir() string {
	wd, _ := os.Getwd()
	return wd
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("[nexus-bot] required env var %s is not set", key)
	}
	return v
}

func handleSignals(cancel context.CancelFunc) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	log.Println("[nexus-bot] shutting down...")
	cancel()
}
