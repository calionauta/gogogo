// The inbox feature's native panel: one file that makes reminders
// answerable from Chat. Same contract as the todo reference
// (features/todo/handlers/genui_panel.go): Render returns the feature's own
// Inbox component, Fill owns the rows, the model picks only the slice.
package handlers

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/a-h/templ"

	notificationscomp "github.com/calionauta/gogogo/features/notifications/components"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
)

const (
	scopeAll    = "all"
	scopeUnread = "unread"
)

// InboxPanel is the app's own inbox_list panel.
type InboxPanel struct {
	Q *dbgen.Queries
}

// NewInboxPanel builds the provider. Nil queries is a wiring bug, not a
// degradation: the notifications handler that owns them panics on nil too.
func NewInboxPanel(q *dbgen.Queries) *InboxPanel {
	if q == nil {
		panic("notifications: NewInboxPanel with nil queries")
	}
	return &InboxPanel{Q: q}
}

// Component implements comp.PanelProvider.
func (p *InboxPanel) Component() string { return "inbox_list" }

// Prompt implements comp.PanelProvider: when the model should pick it.
func (p *InboxPanel) Prompt() string {
	return "inbox_list {scope: unread|all} whenever the request involves the user's " +
		"reminders or inbox (list them, find a reminder, plan around them). Renders " +
		"their REAL rows, so it is the only component allowed to answer with inbox " +
		"content. scope: unread ONLY when the user asks for what is still pending; " +
		"all otherwise. Always send scope inside props."
}

// HomePage implements comp.PanelHomer: the page that owns this data in full.
func (p *InboxPanel) HomePage() string { return "/notifications" }

// Aliases implements comp.PanelAliaiser.
func (p *InboxPanel) Aliases() []string { return []string{"inbox", "reminders", "reminder_list"} }

// Writable implements comp.PanelProvider: the model picks the scope only.
func (p *InboxPanel) Writable() []string { return []string{"scope"} }

// Fill implements comp.PanelProvider. The list is strictly owner-scoped
// (ListNotificationsForOwner) — nobody else's reminders are ever visible.
func (p *InboxPanel) Fill(ctx context.Context, owner string, props map[string]any) (map[string]any, error) {
	scope, _ := props["scope"].(string)
	if scope != scopeUnread && scope != scopeAll {
		return nil, fmt.Errorf("inbox panel: scope %q is not one of [unread all]", scope)
	}
	rows, err := p.Q.ListNotificationsForOwner(ctx, dbgen.ListNotificationsForOwnerParams{
		Owner: owner, Limit: listLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("notifications: list for genui panel: %w", err)
	}
	unread, err := p.Q.CountUnreadInApp(ctx, owner)
	if err != nil {
		return nil, fmt.Errorf("notifications: count for genui panel: %w", err)
	}
	items := make([]notificationscomp.Item, 0, len(rows))
	for _, row := range rows {
		isUnread := !row.ReadAt.Valid
		if scope == scopeUnread && !isUnread {
			continue
		}
		items = append(items, notificationscomp.Item{
			ID:      row.ID,
			Subject: row.Subject,
			Body:    row.Body,
			When:    friendlyTime(row.Created),
			Unread:  isUnread,
		})
	}
	data := notificationscomp.PageData{Items: items, Unread: int(unread), Wired: true}
	return map[string]any{"scope": scope, "data": data}, nil
}

// Render implements comp.PanelProvider: the feature's own Inbox component.
func (p *InboxPanel) Render(props map[string]any) (templ.Component, error) {
	scope, _ := props["scope"].(string)
	if scope != scopeUnread && scope != scopeAll {
		return nil, fmt.Errorf("inbox panel: scope %q is not one of [unread all]", scope)
	}
	data, ok := props["data"].(notificationscomp.PageData)
	if !ok {
		blob, merr := json.Marshal(props["data"])
		if merr != nil {
			return nil, fmt.Errorf("inbox panel: data codec: %w", merr)
		}
		if uerr := json.Unmarshal(blob, &data); uerr != nil {
			return nil, fmt.Errorf("inbox panel: data codec: %w", uerr)
		}
	}
	return notificationscomp.Inbox(data), nil
}
