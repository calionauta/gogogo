-- The recipe's own table. `ext_<id>_` is the boundary the installer and the
-- runtime both enforce: a recipe never owns a core object, and uninstall has to
-- know exactly which rows came from where.
--
-- `corrected` is the outcome's counter signal, and it is a fact of the order
-- rather than telemetry: it is set when a preview was shown and the confirmed
-- order differed from it. That single column is why this recipe needs no event
-- table to answer "was the order right without a repair".

CREATE TABLE ext_kiosk_orders (
    id           TEXT    PRIMARY KEY,
    owner        TEXT    NOT NULL REFERENCES users(id),
    drink        TEXT    NOT NULL,
    size         TEXT    NOT NULL,
    milk         TEXT    NOT NULL DEFAULT '',
    extras       TEXT    NOT NULL DEFAULT '',
    price_cents  INTEGER NOT NULL,
    corrected    INTEGER NOT NULL DEFAULT 0,
    confirmed_at TEXT    NOT NULL
);

-- Both the dashboard metric and the list read one owner's rows newest first;
-- this is the access path they share.
CREATE INDEX ext_kiosk_orders_owner ON ext_kiosk_orders (owner, confirmed_at);
