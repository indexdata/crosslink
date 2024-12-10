WITH auth_isil AS (  
  INSERT INTO authorities (symbol)
  VALUES ('ISIL')
  RETURNING id
), auth_rs AS (
  INSERT INTO authorities (symbol)
  VALUES ('RESHARE')
  RETURNING id
), new_entry AS (
  INSERT INTO entries (name, description, contact_name, email_address)
  VALUES ('Some Institution', 'Some library sort of place', 'Bob', 'bob@someinst.edu')
  RETURNING id
)
INSERT INTO symbols (symbol, authority, owner)
VALUES
  ('CA-SMINST', (SELECT id FROM auth_isil), (SELECT id FROM new_entry)),
  ('SOMEINST', (SELECT id FROM auth_rs), (SELECT id FROM new_entry))
;
