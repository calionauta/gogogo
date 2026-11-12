-- Queries over the recipe's own table. sqlc reads this file because the
-- installed copy lands in internal/db/queries/, which sqlc.yaml already scans;
-- the DDL it describes is the flat mirror `recipe install` writes to
-- internal/db/recipe_schema/coffee__0001_init.sql.
--
-- Every statement is owner-scoped: the caller's id is a parameter, never a value
-- the client could supply.

-- name: ListCoffeeOrders :many
SELECT id, owner, drink, size, milk, extras, price_cents, corrected, confirmed_at
FROM ext_coffee_orders
WHERE owner = ?
ORDER BY confirmed_at DESC
LIMIT ?;

-- name: CreateCoffeeOrder :exec
INSERT INTO ext_coffee_orders
    (id, owner, drink, size, milk, extras, price_cents, corrected, confirmed_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);
