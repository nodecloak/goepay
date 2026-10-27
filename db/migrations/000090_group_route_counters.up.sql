-- The original group index is shared across payment types, never a pay-roll ID.
-- Default group 0 has no backoffice row; positive groups keep a cascading FK.
CREATE TABLE group_route_counters (
 group_id bigint PRIMARY KEY CHECK(group_id>=0),
 group_record_id bigint UNIQUE REFERENCES backoffice_records(id) ON DELETE CASCADE,
 cursor bigint NOT NULL DEFAULT 0 CHECK(cursor>=0),
 assigned bigint NOT NULL DEFAULT 0 CHECK(assigned>=0),
 CHECK((group_id=0 AND group_record_id IS NULL) OR
       (group_id>0 AND group_record_id IS NOT NULL AND group_record_id=group_id))
);
