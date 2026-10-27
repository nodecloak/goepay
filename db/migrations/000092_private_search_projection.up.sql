-- A rebuildable search projection only. No original recipient, proof, amount,
-- journal, authorization or provider intent is modified by this migration.
CREATE TABLE private_search_projections (
 source_kind text NOT NULL CHECK(source_kind IN ('settlement_config','transfer_intent')),
 source_id bigint NOT NULL CHECK(source_id>0),merchant_id bigint NOT NULL REFERENCES merchants(id),
 source_hash bytea NOT NULL CHECK(octet_length(source_hash)=32),
 account_terms text[] NOT NULL,name_terms text[] NOT NULL,remark_terms text[] NOT NULL,
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 PRIMARY KEY(source_kind,source_id),
 CHECK(cardinality(account_terms)<=8192 AND cardinality(name_terms)<=8192 AND cardinality(remark_terms)<=8192)
);
CREATE INDEX private_search_owner ON private_search_projections(source_kind,merchant_id,source_id);
CREATE INDEX private_search_account ON private_search_projections USING gin(account_terms);
CREATE INDEX private_search_name ON private_search_projections USING gin(name_terms);
CREATE INDEX private_search_remark ON private_search_projections USING gin(remark_terms);
CREATE FUNCTION private_search_source_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.source_kind='settlement_config' THEN
  IF NOT EXISTS(SELECT 1 FROM settlement_recipient_configs WHERE id=NEW.source_id AND merchant_id=NEW.merchant_id) THEN RAISE EXCEPTION 'private search source mismatch'; END IF;
 ELSE
  IF NOT EXISTS(SELECT 1 FROM transfers t JOIN provider_transfer_attempts a ON a.transfer_id=t.id WHERE t.id=NEW.source_id AND t.merchant_id=NEW.merchant_id) THEN RAISE EXCEPTION 'private search source mismatch'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER private_search_source BEFORE INSERT OR UPDATE ON private_search_projections FOR EACH ROW EXECUTE FUNCTION private_search_source_guard();
