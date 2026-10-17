-- 0011_search — the search plugin's inverted index.
--
-- Populated only while a consumer indexes something (SEARCH_MODE=bm25):
-- the other two layers (substring, fuzzy) add no table and no write path.
--
-- Denormalized on purpose: `length` (the field's token count) rides every
-- posting row instead of a separate documents table, because BM25 needs it
-- per document and a second table would mean a join plus a second thing to
-- keep in sync with the rows.
--
-- The primary key makes an index write idempotent (re-indexing replaces a
-- row's postings instead of duplicating them). The lookup index is exactly
-- the query BM25 runs — owner + field + terms — so a search never scans
-- another owner's rows: the same authorization boundary the consumer's own
-- query has.

CREATE TABLE search_postings (
    owner  TEXT    NOT NULL,
    kind   TEXT    NOT NULL,
    doc_id TEXT    NOT NULL,
    field  TEXT    NOT NULL,
    term   TEXT    NOT NULL,
    count  INTEGER NOT NULL,
    length INTEGER NOT NULL,
    PRIMARY KEY (owner, kind, doc_id, field, term)
);

CREATE INDEX search_postings_lookup ON search_postings (owner, field, term);
