ALTER TABLE peer
    ADD COLUMN broker_mode VARCHAR;

UPDATE peer
    SET vendor =
            CASE
                WHEN url ILIKE '%alma.exlibrisgroup.co%' THEN 'Alma'
                WHEN url ILIKE '%/rs/externalApi/iso18626%' THEN 'ReShare'
                ELSE 'Unknown'
                END
WHERE vendor = 'api';

UPDATE peer
SET broker_mode =
        CASE
            WHEN url ILIKE '%/rs/externalApi/iso18626%' THEN 'transparent'
            ELSE 'opaque'
            END
WHERE broker_mode IS NULL;

ALTER TABLE peer
    ALTER COLUMN broker_mode SET NOT NULL;
