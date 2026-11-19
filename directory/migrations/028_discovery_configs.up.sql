CREATE TABLE discovery_configs (
  id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
  entry uuid NOT NULL UNIQUE REFERENCES entries (id) ON DELETE CASCADE,
  query_url text,
  item_link_template text
);
