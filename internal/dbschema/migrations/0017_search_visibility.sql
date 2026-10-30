-- Search postings learn the row's visibility, so BM25 can include public
-- rows without dropping the owner scope. Existing postings default to
-- private; the UPDATE backfills public todo rows from the truth table.
ALTER TABLE search_postings ADD COLUMN visibility TEXT NOT NULL DEFAULT 'private';

CREATE INDEX search_postings_visible ON search_postings (visibility, field, term);

UPDATE search_postings
   SET visibility = 'public'
 WHERE kind = 'todo'
   AND EXISTS (
       SELECT 1 FROM todos
        WHERE todos.id = search_postings.doc_id
          AND todos.owner = search_postings.owner
          AND todos.visibility = 'public'
   );
