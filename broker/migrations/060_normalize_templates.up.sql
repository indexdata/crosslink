-- Replace placeholders in template.subject and template.body
-- Old: {{batchQuery}}, {{actualCount}}, {{fullCount}}
-- New: {{.BatchQuery}}, {{.ActualCount}}, {{.FullCount}}
UPDATE template
SET
    subject = REPLACE(
            REPLACE(
                    REPLACE(subject, '{{batchQuery}}', '{{.BatchQuery}}'),
                    '{{actualCount}}', '{{.ActualCount}}'
            ),
            '{{fullCount}}', '{{.FullCount}}'
              ),
    body    = REPLACE(
            REPLACE(
                    REPLACE(body, '{{batchQuery}}', '{{.BatchQuery}}'),
                    '{{actualCount}}', '{{.ActualCount}}'
            ),
            '{{fullCount}}', '{{.FullCount}}'
              )
WHERE
    subject LIKE '%{{batchQuery}}%'
   OR subject LIKE '%{{actualCount}}%'
   OR subject LIKE '%{{fullCount}}%'
   OR body LIKE '%{{batchQuery}}%'
   OR body LIKE '%{{actualCount}}%'
   OR body LIKE '%{{fullCount}}%';