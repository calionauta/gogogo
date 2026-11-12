// The coffee recipe's write half.
//
// Confirm is here rather than in the handler because it is the only place the
// outcome's counter signal is decided: an order is `corrected` when a preview was
// shown and the confirmed order differs from it. Keeping that rule in the
// service means the handler cannot forget it and a test can call it without a
// request.
package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/calionauta/gogogo/features/coffee"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

// Orders owns the recipe's rows.
type Orders struct {
	q *dbgen.Queries
}

// NewOrders builds the slice. Nil queries is a wiring bug, not a degradation:
// the handler would dereference it on the first write.
func NewOrders(q *dbgen.Queries) *Orders {
	if q == nil {
		panic("coffee: NewOrders with nil queries")
	}
	return &Orders{q: q}
}

// Confirm persists one order for one owner.
//
// `previewed` is the serialization the customer was shown before confirming. An
// empty value means nothing was previewed, so no divergence can be observed and
// the order is recorded as not corrected — the honest reading, not a guess.
func (s *Orders) Confirm(ctx context.Context, owner string, order coffee.Order, previewed string) error {
	if problems := coffee.Validate(order); len(problems) > 0 {
		return fmt.Errorf("coffee: %s", strings.Join(problems, "; "))
	}
	return s.q.CreateCoffeeOrder(ctx, dbgen.CreateCoffeeOrderParams{
		ID:          dbschema.NewID(),
		Owner:       owner,
		Drink:       order.Drink,
		Size:        order.Size,
		Milk:        order.Milk,
		Extras:      strings.Join(order.Extras, "+"),
		PriceCents:  int64(coffee.PriceCents(order)),
		Corrected:   boolToInt(Corrected(previewed, order)),
		ConfirmedAt: time.Now().UTC().Format(time.RFC3339),
	})
}

// List returns one owner's newest orders. The limit is capped here rather than in
// SQL so a caller cannot ask for the whole table through a bigger number.
func (s *Orders) List(ctx context.Context, owner string, limit int) ([]dbgen.ExtCoffeeOrder, error) {
	if limit <= 0 || limit > 50 {
		limit = 50
	}
	return s.q.ListCoffeeOrders(ctx, dbgen.ListCoffeeOrdersParams{
		Owner: owner,
		Limit: int64(limit),
	})
}

// Corrected is the outcome's counter signal, as one pure function: a preview was
// shown and the confirmed order differs from it. Empty previewed means nothing
// was shown, so no divergence can be observed — the honest reading of "no data",
// not a claim that the order was right.
func Corrected(previewed string, order coffee.Order) bool {
	return previewed != "" && previewed != coffee.Serialize(order)
}

// boolToInt stores a boolean in the schema's INTEGER column, which is how the
// rest of the core schema spells one.
func boolToInt(v bool) int64 {
	if v {
		return 1
	}
	return 0
}
