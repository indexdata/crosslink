ALTER TABLE catalog_configs
    ADD COLUMN metadata_marc21_publisher text,
    ADD COLUMN metadata_marc21_publication_date text,
    ADD COLUMN metadata_marc21_place_of_publication text;
