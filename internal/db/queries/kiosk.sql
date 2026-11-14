-- Queries over the recipe's own table. sqlc reads this file because the
-- installed copy lands in internal/db/queries/, which sqlc.yaml already scans;
-- the DDL it describes is the flat mirror `recipe install` writes to
-- internal/db/recipe_schema/kiosk__0001_init.sql.
--
-- Every statement is owner-scoped: the caller's id is a parameter, never a value
-- the client could supply.

-- name: ListKioskOrders :many
SELECT id, owner, drink, size, milk, extras, price_cents, corrected, confirmed_at
FROM ext_kiosk_orders
WHERE owner = ?
ORDER BY confirmed_at DESC
LIMIT ?;

-- name: CreateKioskOrder :exec
INSERT INTO ext_kiosk_orders
    (id, owner, drink, size, milk, extras, price_cents, corrected, confirmed_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);
