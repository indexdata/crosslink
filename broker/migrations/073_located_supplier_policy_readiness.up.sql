-- NULL denotes legacy/unknown readiness; policy-bearing suppliers must be checked again.
ALTER TABLE located_supplier ADD COLUMN holdings_policy_resolved BOOLEAN;
