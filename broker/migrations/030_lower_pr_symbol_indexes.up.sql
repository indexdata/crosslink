CREATE INDEX idx_pr_requester_symbol_lower ON patron_request (lower(requester_symbol) varchar_pattern_ops);
CREATE INDEX idx_pr_supplier_symbol_lower ON patron_request (lower(supplier_symbol) varchar_pattern_ops);
CREATE INDEX idx_pr_requester_req_id_lower ON patron_request (lower(requester_req_id) varchar_pattern_ops);
