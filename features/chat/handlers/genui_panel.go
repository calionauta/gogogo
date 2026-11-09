// The chat feature's native panel: one file that makes the conversation
// history answerable from Chat itself. Same contract as the todo reference
// (features/todo/handlers/genui_panel.go): Render returns the feature's own
// thread component, Fill owns the rows, the model picks only the slice size.
package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/a-h/templ"

	chatcomp "github.com/calionauta/gogogo/features/chat/components"
	"github.com/calionauta/gogogo/features/chat/store"
)

// ChatPanel is the app's own chat_history panel.
type ChatPanel struct {
	Store store.MessageStore
}

// NewChatPanel builds the provider. Nil store is a wiring bug, not a
// degradation: the chat handler that owns it panics on nil too.
func NewChatPanel(s store.MessageStore) *ChatPanel {
	if s == nil {
		panic("chat: NewChatPanel with nil store")
	}
	return &ChatPanel{Store: s}
}

// Component implements comp.PanelProvider.
func (p *ChatPanel) Component() string { return "chat_history" }

// Prompt implements comp.PanelProvider: when the model should pick it.
func (p *ChatPanel) Prompt() string {
	return "chat_history {limit: 5|10|20} whenever the request involves the user's " +
		"recent conversation (what did we discuss, what did I ask before, summarize " +
		"the thread). Renders their REAL history, so it is the only component " +
		"allowed to answer with conversation content. limit is how many recent " +
		"messages to show: 5 for a quick recall, 20 for a full summary. " +
		"Always send limit inside props."
}

// HomePage implements comp.PanelHomer: the page that owns this data in full.
func (p *ChatPanel) HomePage() string { return "/chat" }

// Aliases implements comp.PanelAliaiser.
func (p *ChatPanel) Aliases() []string { return []string{"history", "conversation"} }

// Writable implements comp.PanelProvider: the model picks the history size only.
func (p *ChatPanel) Writable() []string { return []string{"limit"} }

// Fill implements comp.PanelProvider: the owner's recent messages, oldest first.
func (p *ChatPanel) Fill(ctx context.Context, owner string, props map[string]any) (map[string]any, error) {
	limit, err := chatLimit(props)
	if err != nil {
		return nil, err
	}
	msgs, err := p.Store.List(ctx, owner)
	if err != nil {
		return nil, fmt.Errorf("chat: list for genui panel: %w", err)
	}
	if len(msgs) > limit {
		msgs = msgs[len(msgs)-limit:]
	}
	return map[string]any{"limit": props["limit"], "rows": msgs}, nil
}

// Render implements comp.PanelProvider: the feature's own thread component.
func (p *ChatPanel) Render(props map[string]any) (templ.Component, error) {
	if _, err := chatLimit(props); err != nil {
		return nil, err
	}
	return chatcomp.ChatThread(panelMessageViews(props), ""), nil
}

// chatLimit validates the model-chosen history size.
func chatLimit(props map[string]any) (int, error) {
	return chatLimitValue(props["limit"])
}

func chatLimitValue(v any) (int, error) {
	s, ok := v.(string)
	if !ok {
		return 0, fmt.Errorf("chat panel: limit %v is not one of [5 10 20]", v)
	}
	n, err := strconv.Atoi(s)
	if err != nil || (n != 5 && n != 10 && n != 20) {
		return 0, fmt.Errorf("chat panel: limit %q is not one of [5 10 20]", s)
	}
	return n, nil
}

// panelMessageViews decodes the `rows` prop in the shapes it reaches Render
// in (direct []store.Message or the wire []any of maps, via Message's JSON
// tags), then maps to the feature's own MessageView shape.
func panelMessageViews(props map[string]any) []store.MessageView {
	msgs := panelMessages(props)
	out := make([]store.MessageView, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, store.MessageView{Role: m.Role, Text: m.Text})
	}
	return out
}

func panelMessages(props map[string]any) []store.Message {
	switch rows := props["rows"].(type) {
	case []store.Message:
		return rows
	case []any:
		out := make([]store.Message, 0, len(rows))
		for _, raw := range rows {
			row, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			blob, merr := json.Marshal(row)
			if merr != nil {
				continue
			}
			var item store.Message
			if uerr := json.Unmarshal(blob, &item); uerr != nil {
				continue
			}
			out = append(out, item)
		}
		return out
	default:
		return nil
	}
}
